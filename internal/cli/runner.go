package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shidetake/clapless/internal/audio"
	audiosync "github.com/shidetake/clapless/internal/sync"
)

const (
	minConfidence = 0.3 // Minimum confidence threshold
)

// Run executes the main synchronization workflow
func Run(config *Config) error {
	fmt.Println("Clapless - Audio Synchronization Tool")
	fmt.Println("======================================")
	fmt.Println()

	// Step 1: Load mixed audio
	fmt.Println("Loading files...")
	mixed, err := loadMixedAudio(config.MixedPath)
	if err != nil {
		return err
	}

	// Step 2: Load local audio files
	localFiles, err := loadLocalAudio(config.LocalPaths)
	if err != nil {
		return err
	}

	// Validate sample rates match
	if err := validateSampleRates(mixed, localFiles); err != nil {
		return err
	}

	fmt.Println()

	// Step 3: Detect offsets in parallel
	fmt.Println("Detecting offsets...")
	offsetResults, err := detectOffsetsParallel(mixed, localFiles, config.SegmentDuration)
	if err != nil {
		return err
	}

	// Step 4: Calculate padding
	fileOffsets, err := audiosync.CalculatePadding(offsetResults, config.LocalPaths, mixed.SampleRate)
	if err != nil {
		return err
	}

	// Display offset results
	for i, fo := range fileOffsets {
		fmt.Printf("  ✓ %s: %s (confidence: %.2f)\n",
			filepath.Base(config.LocalPaths[i]),
			audiosync.FormatOffsetSeconds(fo.OffsetSeconds),
			fo.Confidence)
	}

	// Check confidence scores
	warnings := audiosync.ValidateConfidence(fileOffsets, minConfidence)
	if len(warnings) > 0 {
		fmt.Println()
		fmt.Println("⚠️  Warnings:")
		for _, warning := range warnings {
			fmt.Printf("  %s\n", warning)
		}
		fmt.Println("  Synchronization may not be accurate. Please verify results.")
	}

	fmt.Println()

	// Step 5: Apply padding and write synced files
	fmt.Println("Calculating synchronization...")
	for i, fo := range fileOffsets {
		if fo.IsEarliest {
			fmt.Printf("  %s: No padding needed (earliest)\n", filepath.Base(config.LocalPaths[i]))
		} else {
			fmt.Printf("  %s: Adding %.3fs silence\n", filepath.Base(config.LocalPaths[i]), fo.PaddingSeconds)
		}
	}

	fmt.Println()
	fmt.Println("Writing synchronized files...")

	for i, fo := range fileOffsets {
		if err := writeSyncedFile(localFiles[i], fo, config.LocalPaths[i]); err != nil {
			return fmt.Errorf("failed to write synced file for %s: %w", config.LocalPaths[i], err)
		}
		outputPath := generateOutputPath(config.LocalPaths[i])
		fmt.Printf("  ✓ %s\n", filepath.Base(outputPath))
	}

	fmt.Println()
	fmt.Println("Synchronization complete!")

	return nil
}

// loadMixedAudio loads the mixed audio file
func loadMixedAudio(path string) (*audio.WAVData, error) {
	mixed, err := audio.LoadWAV(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load mixed audio: %w", err)
	}

	fmt.Printf("  ✓ Mixed: %s (%d channels, %d Hz, %s)\n",
		filepath.Base(path),
		mixed.Channels,
		mixed.SampleRate,
		mixed.DurationString())

	return mixed, nil
}

// loadLocalAudio loads all local audio files
func loadLocalAudio(paths []string) ([]*audio.WAVData, error) {
	localFiles := make([]*audio.WAVData, len(paths))

	for i, path := range paths {
		local, err := audio.LoadWAV(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load local audio %s: %w", path, err)
		}

		fmt.Printf("  ✓ Local %d: %s (%d channels, %d Hz, %s)\n",
			i+1,
			filepath.Base(path),
			local.Channels,
			local.SampleRate,
			local.DurationString())

		localFiles[i] = local
	}

	return localFiles, nil
}

// validateSampleRates ensures all files have the same sample rate
func validateSampleRates(mixed *audio.WAVData, localFiles []*audio.WAVData) error {
	for i, local := range localFiles {
		if local.SampleRate != mixed.SampleRate {
			return fmt.Errorf("sample rate mismatch: mixed (%d Hz) vs local %d (%d Hz)",
				mixed.SampleRate, i+1, local.SampleRate)
		}
	}
	return nil
}

// detectOffsetsParallel detects offsets for all local files in parallel
func detectOffsetsParallel(mixed *audio.WAVData, localFiles []*audio.WAVData, segmentDuration int) ([]*audiosync.OffsetResult, error) {
	// Convert mixed to mono for correlation
	mixedMono := audio.ToMono(mixed.Data, mixed.Channels)

	type result struct {
		index  int
		offset *audiosync.OffsetResult
		err    error
	}

	results := make(chan result, len(localFiles))
	var wg sync.WaitGroup

	// Launch goroutines for parallel processing
	for i, local := range localFiles {
		wg.Add(1)
		go func(idx int, localData *audio.WAVData) {
			defer wg.Done()

			// Convert to mono
			localMono := audio.ToMono(localData.Data, localData.Channels)

			// Detect offset
			offset, err := audiosync.DetectOffset(mixedMono, localMono, mixed.SampleRate, segmentDuration)
			results <- result{
				index:  idx,
				offset: offset,
				err:    err,
			}
		}(i, local)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(results)

	// Collect results
	offsetResults := make([]*audiosync.OffsetResult, len(localFiles))
	for r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("offset detection failed for file %d: %w", r.index+1, r.err)
		}
		offsetResults[r.index] = r.offset
	}

	return offsetResults, nil
}

// writeSyncedFile writes a synchronized audio file with padding
func writeSyncedFile(localData *audio.WAVData, fo *audiosync.FileOffset, originalPath string) error {
	// Prepend silence if needed
	syncedData := localData.Data
	if fo.PaddingSamples > 0 {
		// For multi-channel audio, we need to prepend silence for each channel
		silenceSamples := fo.PaddingSamples * localData.Channels
		syncedData = audio.PrependSilence(localData.Data, silenceSamples)
	}

	// Generate output path
	outputPath := generateOutputPath(originalPath)

	// Write synced WAV file
	if err := audio.WriteWAV(outputPath, syncedData, localData.SampleRate, localData.Channels, localData.BitDepth); err != nil {
		return err
	}

	return nil
}

// generateOutputPath creates the output file path with _synced suffix
func generateOutputPath(originalPath string) string {
	dir := filepath.Dir(originalPath)
	base := filepath.Base(originalPath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)

	return filepath.Join(dir, nameWithoutExt+"_synced"+ext)
}
