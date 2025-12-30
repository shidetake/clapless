package sync

import (
	"fmt"

	"github.com/shidetake/clapless/internal/audio"
)

// OverlapRegion represents the temporal region where all files have data after coarse alignment
type OverlapRegion struct {
	StartSample int     // Start position in samples (on aligned timeline)
	EndSample   int     // End position in samples
	DurationSec float64 // Duration in seconds
}

// FinetuneResult contains the result of fine-tuning for a single file
type FinetuneResult struct {
	FineAdjustmentSamples int     // Adjustment to ADD to coarse offset (positive = shift later)
	FineAdjustmentSeconds float64 // Adjustment to ADD to coarse offset (positive = shift later)
	Confidence            float64 // Confidence score
	SegmentUsed           OverlapRegion
	Skipped               bool
	SkipReason            string
}

// extractSegment extracts a portion of audio data
func extractSegment(data []float64, startSample, endSample int) ([]float64, error) {
	if startSample < 0 || endSample > len(data) || startSample >= endSample {
		return nil, fmt.Errorf("invalid segment bounds: [%d, %d) for data length %d",
			startSample, endSample, len(data))
	}

	segment := make([]float64, endSample-startSample)
	copy(segment, data[startSample:endSample])
	return segment, nil
}

// findOverlappingRegion determines where all files have data after coarse alignment
func findOverlappingRegion(
	localFiles []*audio.WAVData,
	fileOffsets []*FileOffset,
	sampleRate int,
) (*OverlapRegion, error) {
	if len(localFiles) == 0 {
		return nil, fmt.Errorf("no local files provided")
	}

	// Calculate start and end positions for each file on the aligned timeline
	var overlapStart int
	var overlapEnd int

	for i, localFile := range localFiles {
		// Convert to mono to get actual sample count
		monoSamples := len(localFile.Data) / localFile.Channels

		// This file starts at its offset and ends at offset + length
		fileStart := fileOffsets[i].OffsetSamples
		fileEnd := fileStart + monoSamples

		if i == 0 {
			overlapStart = fileStart
			overlapEnd = fileEnd
		} else {
			// Overlap starts at the latest start time
			if fileStart > overlapStart {
				overlapStart = fileStart
			}
			// Overlap ends at the earliest end time
			if fileEnd < overlapEnd {
				overlapEnd = fileEnd
			}
		}
	}

	// Validate overlap exists
	if overlapEnd <= overlapStart {
		return nil, fmt.Errorf("no overlapping region found after coarse alignment (start: %d, end: %d)",
			overlapStart, overlapEnd)
	}

	return &OverlapRegion{
		StartSample: overlapStart,
		EndSample:   overlapEnd,
		DurationSec: float64(overlapEnd-overlapStart) / float64(sampleRate),
	}, nil
}

// selectFinetuneSegment chooses the segment to use for fine-tuning
func selectFinetuneSegment(
	overlap *OverlapRegion,
	targetDuration float64, // Target duration in seconds (e.g., 60.0)
	minDuration float64,    // Minimum acceptable duration (e.g., 30.0)
	sampleRate int,
) (startSample, endSample int, err error) {
	targetSamples := int(targetDuration * float64(sampleRate))
	minSamples := int(minDuration * float64(sampleRate))
	overlapSamples := overlap.EndSample - overlap.StartSample

	// Check if overlap is too small
	if overlapSamples < minSamples {
		return 0, 0, fmt.Errorf("overlap duration %.2fs is less than minimum %.2fs",
			overlap.DurationSec, minDuration)
	}

	// If overlap is larger than target, use centered segment
	if overlapSamples >= targetSamples {
		center := overlap.StartSample + overlapSamples/2
		halfTarget := targetSamples / 2
		return center - halfTarget, center - halfTarget + targetSamples, nil
	}

	// Use entire overlap
	return overlap.StartSample, overlap.EndSample, nil
}

// recalculatePadding recalculates padding based on final offsets
func recalculatePadding(fileOffsets []*FileOffset, sampleRate int) ([]*FileOffset, error) {
	if len(fileOffsets) == 0 {
		return nil, fmt.Errorf("no file offsets provided")
	}

	// Find minimum final offset (earliest file)
	minOffset := fileOffsets[0].FinalOffsetSamples
	for _, fo := range fileOffsets {
		if fo.FinalOffsetSamples < minOffset {
			minOffset = fo.FinalOffsetSamples
		}
	}

	// Update padding for each file
	for i := range fileOffsets {
		padding := fileOffsets[i].FinalOffsetSamples - minOffset
		fileOffsets[i].PaddingSamples = padding
		fileOffsets[i].PaddingSeconds = float64(padding) / float64(sampleRate)
		fileOffsets[i].IsEarliest = (fileOffsets[i].FinalOffsetSamples == minOffset)
	}

	return fileOffsets, nil
}

// FinetuneOffsets performs fine-tuning on coarsely aligned files
func FinetuneOffsets(
	mixed []float64,
	localFiles []*audio.WAVData,
	fileOffsets []*FileOffset,
	sampleRate int,
) ([]*FileOffset, error) {
	// Step 1: Find overlapping region
	overlap, err := findOverlappingRegion(localFiles, fileOffsets, sampleRate)
	if err != nil {
		return nil, fmt.Errorf("failed to find overlapping region: %w", err)
	}

	// Step 2: Select segment for fine-tuning (target 60s, minimum 30s)
	segStart, segEnd, err := selectFinetuneSegment(overlap, 60.0, 30.0, sampleRate)
	if err != nil {
		// Overlap too small, skip fine-tuning for all files
		for i := range fileOffsets {
			fileOffsets[i].FinetuneResult = &FinetuneResult{
				Skipped:    true,
				SkipReason: err.Error(),
			}
			fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples
			fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds
		}
		return fileOffsets, nil
	}

	// Step 3: Extract mixed segment
	mixedSegment, err := extractSegment(mixed, segStart, segEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to extract mixed segment: %w", err)
	}

	// Step 4: Fine-tune each local file
	for i, localFile := range localFiles {
		// Convert to mono
		localMono := audio.ToMono(localFile.Data, localFile.Channels)

		// Calculate where this file's segment should be extracted
		// The segment is at [segStart, segEnd) on the aligned timeline
		// This file starts at fileOffsets[i].OffsetSamples
		localSegStart := segStart - fileOffsets[i].OffsetSamples
		localSegEnd := segEnd - fileOffsets[i].OffsetSamples

		// Validate bounds
		if localSegStart < 0 || localSegEnd > len(localMono) {
			fileOffsets[i].FinetuneResult = &FinetuneResult{
				Skipped:    true,
				SkipReason: fmt.Sprintf("segment out of bounds [%d, %d) for file length %d",
					localSegStart, localSegEnd, len(localMono)),
			}
			fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples
			fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds
			continue
		}

		// Extract local segment
		localSegment, err := extractSegment(localMono, localSegStart, localSegEnd)
		if err != nil {
			fileOffsets[i].FinetuneResult = &FinetuneResult{
				Skipped:    true,
				SkipReason: fmt.Sprintf("extraction failed: %v", err),
			}
			fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples
			fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds
			continue
		}

		// Run cross-correlation without downsampling (downsampleFactor = 1)
		fineResult, err := DetectOffset(
			mixedSegment,
			localSegment,
			sampleRate,
			0, // segmentDuration not used
			1, // downsampleFactor = 1 (no downsampling)
		)
		if err != nil {
			fileOffsets[i].FinetuneResult = &FinetuneResult{
				Skipped:    true,
				SkipReason: fmt.Sprintf("correlation failed: %v", err),
			}
			fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples
			fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds
			continue
		}

		// Store fine-tuning result
		// FineAdjustmentSamples is the adjustment to ADD to the coarse offset (sign-inverted from DetectOffset)
		fileOffsets[i].FinetuneResult = &FinetuneResult{
			FineAdjustmentSamples: -fineResult.OffsetSamples,
			FineAdjustmentSeconds: -fineResult.OffsetSeconds,
			Confidence:            fineResult.Confidence,
			SegmentUsed: OverlapRegion{
				StartSample: segStart,
				EndSample:   segEnd,
				DurationSec: float64(segEnd-segStart) / float64(sampleRate),
			},
			Skipped: false,
		}

		// Merge coarse and fine offsets
		// Time direction convention: positive = shift later (backward in time), negative = shift earlier (forward in time)
		// - DetectOffset returns: positive = local segment is ahead (too early)
		// - If local is ahead, we need to REDUCE the offset to shift it earlier
		// - FineAdjustmentSamples stores the adjustment to ADD to the offset
		// - Example: coarse=1000, DetectOffset=+10 (too early) -> adjustment=-10 -> final=1000+(-10)=990
		fileOffsets[i].FineAdjustmentSamples = -fineResult.OffsetSamples
		fileOffsets[i].FineAdjustmentSeconds = -fineResult.OffsetSeconds
		fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples + fileOffsets[i].FineAdjustmentSamples
		fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds + fileOffsets[i].FineAdjustmentSeconds
	}

	// Step 5: Recalculate padding based on final offsets
	return recalculatePadding(fileOffsets, sampleRate)
}
