package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Config holds the parsed command-line configuration
type Config struct {
	MixedPath        string
	LocalPaths       []string
	SegmentDuration  int // Segment duration in seconds for correlation (default: 600)
	DownsampleFactor int // Downsample factor for coarse search (default: 50)
}

var (
	mixedPath        string
	segmentDuration  int
	downsampleFactor int
)

var rootCmd = &cobra.Command{
	Use:   "clapless [flags] <local1.wav> <local2.wav> [local3.wav ...]",
	Short: "Audio Synchronization Tool",
	Long: `Clapless - Audio Synchronization Tool

Automatically synchronize local podcast recordings with a mixed source.

Example:
  clapless --mixed podcast_mix.wav alice.wav bob.wav
  clapless -m podcast_mix.wav -d 100 alice.wav bob.wav

Output:
  Creates synchronized files with _synced suffix:
    alice_synced.wav
    bob_synced.wav`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate mixed path
		if mixedPath == "" {
			return fmt.Errorf("--mixed flag is required")
		}

		// Validate minimum number of local files
		if len(args) < 2 {
			return fmt.Errorf("at least 2 local audio files are required, got %d", len(args))
		}

		// Validate file existence and format
		if err := validateFile(mixedPath); err != nil {
			return fmt.Errorf("mixed file error: %w", err)
		}

		for i, path := range args {
			if err := validateFile(path); err != nil {
				return fmt.Errorf("local file %d (%s) error: %w", i+1, path, err)
			}
		}

		// Validate segment duration
		if segmentDuration <= 0 {
			return fmt.Errorf("segment duration must be positive, got %d", segmentDuration)
		}

		// Validate downsample factor
		if downsampleFactor < 1 {
			return fmt.Errorf("downsample factor must be >= 1, got %d", downsampleFactor)
		}

		// Build config
		config := &Config{
			MixedPath:        mixedPath,
			LocalPaths:       args,
			SegmentDuration:  segmentDuration,
			DownsampleFactor: downsampleFactor,
		}

		// Run synchronization workflow
		return Run(config)
	},
	SilenceUsage: true, // Don't show usage on errors during execution
}

func init() {
	rootCmd.Flags().StringVarP(&mixedPath, "mixed", "m", "", "Path to the mixed audio file (required)")
	rootCmd.Flags().IntVar(&segmentDuration, "segment-duration", 600, "Segment duration in seconds for correlation")
	rootCmd.Flags().IntVarP(&downsampleFactor, "downsample", "d", 50, "Downsample factor for coarse offset search (higher = faster but less accurate)")

	rootCmd.MarkFlagRequired("mixed")
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
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
