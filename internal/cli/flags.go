package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds the parsed command-line configuration
type Config struct {
	MixedPath       string
	LocalPaths      []string
	SegmentDuration int // Segment duration in seconds for correlation (default: 600)
}

// ParseFlags parses command-line flags and validates input
func ParseFlags() (*Config, error) {
	// Define flags
	mixedPath := flag.String("mixed", "", "Path to the mixed audio file (required)")
	segmentDuration := flag.Int("segment-duration", 600, "Segment duration in seconds for correlation (default: 600 = 10 minutes)")
	flag.Parse()

	// Get positional arguments (local audio files)
	localPaths := flag.Args()

	// Validate mixed path
	if *mixedPath == "" {
		return nil, fmt.Errorf("--mixed flag is required\n\nUsage: clapless --mixed <mixed.wav> <local1.wav> <local2.wav> ...")
	}

	// Validate minimum number of local files
	if len(localPaths) < 2 {
		return nil, fmt.Errorf("at least 2 local audio files are required, got %d\n\nUsage: clapless --mixed <mixed.wav> <local1.wav> <local2.wav> ...", len(localPaths))
	}

	// Validate file existence and format
	if err := validateFile(*mixedPath); err != nil {
		return nil, fmt.Errorf("mixed file error: %w", err)
	}

	for i, path := range localPaths {
		if err := validateFile(path); err != nil {
			return nil, fmt.Errorf("local file %d (%s) error: %w", i+1, path, err)
		}
	}

	// Validate segment duration
	if *segmentDuration <= 0 {
		return nil, fmt.Errorf("segment duration must be positive, got %d", *segmentDuration)
	}

	return &Config{
		MixedPath:       *mixedPath,
		LocalPaths:      localPaths,
		SegmentDuration: *segmentDuration,
	}, nil
}

// validateFile checks if a file exists and has .wav extension
func validateFile(path string) error {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access file: %w", err)
	}

	// Check if it's a regular file
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", path)
	}

	// Check if it has .wav extension
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".wav" {
		return fmt.Errorf("file must be WAV format (got %s): %s", ext, path)
	}

	return nil
}

// PrintUsage prints usage information
func PrintUsage() {
	fmt.Println("Clapless - Audio Synchronization Tool")
	fmt.Println("======================================")
	fmt.Println()
	fmt.Println("Automatically synchronize local podcast recordings with a mixed source.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  clapless --mixed <mixed.wav> <local1.wav> <local2.wav> [local3.wav ...]")
	fmt.Println()
	fmt.Println("Options:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Example:")
	fmt.Println("  clapless --mixed podcast_mix.wav alice.wav bob.wav")
	fmt.Println()
	fmt.Println("Output:")
	fmt.Println("  Creates synchronized files with _synced suffix:")
	fmt.Println("    alice_synced.wav")
	fmt.Println("    bob_synced.wav")
}
