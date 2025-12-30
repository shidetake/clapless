package sync

import (
	"fmt"
	"math"
)

// FileOffset represents the offset and padding information for a single file
type FileOffset struct {
	Path            string
	OffsetSamples   int     // Original detected offset
	OffsetSeconds   float64 // Original offset in seconds
	PaddingSamples  int     // Silence to prepend
	PaddingSeconds  float64 // Silence in seconds
	Confidence      float64 // Detection confidence
	IsEarliest      bool    // Whether this is the earliest file
}

// CalculatePadding calculates the silence padding needed for each file
// to synchronize all files based on the earliest one
func CalculatePadding(results []*OffsetResult, filePaths []string, sampleRate int) ([]*FileOffset, error) {
	if len(results) != len(filePaths) {
		return nil, fmt.Errorf("mismatch between results (%d) and file paths (%d)", len(results), len(filePaths))
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no offset results provided")
	}

	// Find the minimum offset (earliest file)
	minOffset := results[0].OffsetSamples
	for _, result := range results {
		if result.OffsetSamples < minOffset {
			minOffset = result.OffsetSamples
		}
	}

	// Calculate padding for each file
	fileOffsets := make([]*FileOffset, len(results))
	for i, result := range results {
		// Padding is the difference between this file's offset and the minimum offset
		// If this file is the earliest (minimum offset), padding is 0
		padding := result.OffsetSamples - minOffset

		fileOffsets[i] = &FileOffset{
			Path:            filePaths[i],
			OffsetSamples:   result.OffsetSamples,
			OffsetSeconds:   result.OffsetSeconds,
			PaddingSamples:  padding,
			PaddingSeconds:  float64(padding) / float64(sampleRate),
			Confidence:      result.Confidence,
			IsEarliest:      result.OffsetSamples == minOffset,
		}
	}

	return fileOffsets, nil
}

// ValidateConfidence checks if all confidence scores meet the minimum threshold
func ValidateConfidence(fileOffsets []*FileOffset, minConfidence float64) []string {
	var warnings []string

	for _, fo := range fileOffsets {
		if fo.Confidence < minConfidence {
			warnings = append(warnings, fmt.Sprintf(
				"%s: low confidence score %.2f (threshold: %.2f)",
				fo.Path, fo.Confidence, minConfidence,
			))
		}
	}

	return warnings
}

// FormatOffsetSeconds formats seconds to a human-readable string with sign
func FormatOffsetSeconds(seconds float64) string {
	absSeconds := math.Abs(seconds)
	sign := ""
	if seconds > 0 {
		sign = "+"
	} else if seconds < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%.3fs", sign, absSeconds)
}
