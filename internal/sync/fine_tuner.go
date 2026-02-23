package sync

import (
	"fmt"
	"math"

	"github.com/shidetake/clapless/internal/audio"
)

const (
	// segmentDurationSec is the duration of each analysis segment in seconds
	segmentDurationSec = 60.0
	// minOverlapDurationSec is the minimum overlap needed for fine-tuning
	minOverlapDurationSec = 30.0
	// driftThresholdPPM is the minimum drift rate to apply correction (10 parts per million)
	driftThresholdPPM = 10e-6
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
	DriftRate             float64 // Drift rate (seconds per second, 0 = no drift)
	DriftCorrectedData    []float64 // Drift-corrected mono audio data (nil if no drift correction)
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

// detectSegmentOffset runs GCC-PHAT on a segment and returns the offset in samples
func detectSegmentOffset(mixed, localMono []float64, segStart, segEnd, localFileOffset, sampleRate int) (int, error) {
	// Extract mixed segment
	mixedSeg, err := extractSegment(mixed, segStart, segEnd)
	if err != nil {
		return 0, fmt.Errorf("failed to extract mixed segment: %w", err)
	}

	// Calculate local segment position
	localSegStart := segStart - localFileOffset
	localSegEnd := segEnd - localFileOffset

	// Validate bounds
	if localSegStart < 0 || localSegEnd > len(localMono) {
		return 0, fmt.Errorf("segment out of bounds [%d, %d) for file length %d",
			localSegStart, localSegEnd, len(localMono))
	}

	localSeg, err := extractSegment(localMono, localSegStart, localSegEnd)
	if err != nil {
		return 0, fmt.Errorf("failed to extract local segment: %w", err)
	}

	// Normalize both segments
	mixedNorm := normalize(mixedSeg)
	localNorm := normalize(localSeg)

	// Run GCC-PHAT
	correlation := crossCorrelateGCCPHAT(mixedNorm, localNorm)

	// Find peak
	peakIdx, _ := findMaxPeak(correlation)

	// Convert peak index to offset (same convention as DetectOffset)
	// Threshold is the actual mixed segment length (not the fixed segmentDurationSec)
	mixedSegLen := segEnd - segStart
	offset := peakIdx
	if peakIdx >= mixedSegLen {
		offset = peakIdx - len(correlation)
	}

	return offset, nil
}

// FinetuneOffsets performs fine-tuning using segment GCC-PHAT with drift detection
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

	segSamples := int(segmentDurationSec * float64(sampleRate))
	minSamples := int(minOverlapDurationSec * float64(sampleRate))
	overlapSamples := overlap.EndSample - overlap.StartSample

	// Check minimum overlap
	if overlapSamples < minSamples {
		for i := range fileOffsets {
			fileOffsets[i].FinetuneResult = &FinetuneResult{
				Skipped:    true,
				SkipReason: fmt.Sprintf("overlap duration %.2fs is less than minimum %.2fs",
					overlap.DurationSec, minOverlapDurationSec),
			}
			fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples
			fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds
		}
		return fileOffsets, nil
	}

	// Step 2: Fine-tune each local file using segment GCC-PHAT
	for i, localFile := range localFiles {
		localMono := audio.ToMono(localFile.Data, localFile.Channels)

		// Determine head segment bounds
		headStart := overlap.StartSample
		headEnd := headStart + segSamples
		if headEnd > overlap.EndSample {
			headEnd = overlap.EndSample
		}

		// Detect offset at head segment
		headOffset, err := detectSegmentOffset(mixed, localMono, headStart, headEnd, fileOffsets[i].OffsetSamples, sampleRate)
		if err != nil {
			fileOffsets[i].FinetuneResult = &FinetuneResult{
				Skipped:    true,
				SkipReason: fmt.Sprintf("head segment: %v", err),
			}
			fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples
			fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds
			continue
		}

		// Fine adjustment from head segment.
		// headOffset > 0 means local is behind mix in this segment → increase offset.
		// headOffset < 0 means local is ahead of mix → decrease offset.
		fineAdjSamples := headOffset
		fineAdjSeconds := float64(fineAdjSamples) / float64(sampleRate)

		// Detect drift using tail segment (only if overlap is long enough for two distinct segments)
		var driftRate float64
		if overlapSamples >= 2*segSamples {
			tailEnd := overlap.EndSample
			tailStart := tailEnd - segSamples

			tailOffset, tailErr := detectSegmentOffset(mixed, localMono, tailStart, tailEnd, fileOffsets[i].OffsetSamples, sampleRate)
			if tailErr == nil {
				// Head and tail positions on the aligned timeline (center of each segment)
				headPosSec := float64(headStart+headEnd) / 2.0 / float64(sampleRate)
				tailPosSec := float64(tailStart+tailEnd) / 2.0 / float64(sampleRate)
				timeDiff := tailPosSec - headPosSec

				if timeDiff > 0 {
					// Drift = difference in detected offsets over time.
					// tailOffset < headOffset means local is getting more ahead over time → local clock is fast.
					// We negate so that driftRate > 0 means local clock is fast (needs compression).
					offsetDiffSec := float64(tailOffset-headOffset) / float64(sampleRate)
					driftRate = -offsetDiffSec / timeDiff
				}
			}
		}

		// Apply drift correction if significant
		var driftCorrectedData []float64
		if math.Abs(driftRate) > driftThresholdPPM {
			driftCorrectedData = resampleWithDrift(localMono, driftRate, sampleRate)
		}

		// Store result
		fileOffsets[i].FinetuneResult = &FinetuneResult{
			FineAdjustmentSamples: fineAdjSamples,
			FineAdjustmentSeconds: fineAdjSeconds,
			DriftRate:             driftRate,
			DriftCorrectedData:    driftCorrectedData,
			SegmentUsed: OverlapRegion{
				StartSample: headStart,
				EndSample:   headEnd,
				DurationSec: float64(headEnd-headStart) / float64(sampleRate),
			},
			Skipped: false,
		}

		fileOffsets[i].FineAdjustmentSamples = fineAdjSamples
		fileOffsets[i].FineAdjustmentSeconds = fineAdjSeconds
		fileOffsets[i].FinalOffsetSamples = fileOffsets[i].OffsetSamples + fineAdjSamples
		fileOffsets[i].FinalOffsetSeconds = fileOffsets[i].OffsetSeconds + fineAdjSeconds
	}

	// Step 3: Recalculate padding based on final offsets
	return recalculatePadding(fileOffsets, sampleRate)
}

// resampleWithDrift corrects clock drift by resampling audio data using linear interpolation.
// driftRate is seconds-per-second: positive means local clock runs fast (samples are too close together).
// Each output sample n maps to input position n * (1 + driftRate).
func resampleWithDrift(data []float64, driftRate float64, sampleRate int) []float64 {
	n := len(data)
	if n == 0 {
		return data
	}

	// Output length: local is stretched/compressed so total duration changes
	outputLen := int(float64(n) / (1.0 + driftRate))
	if outputLen <= 0 {
		return data
	}

	result := make([]float64, outputLen)
	scale := 1.0 + driftRate

	for i := 0; i < outputLen; i++ {
		srcPos := float64(i) * scale
		idx := int(srcPos)
		frac := srcPos - float64(idx)

		if idx+1 < n {
			result[i] = data[idx]*(1-frac) + data[idx+1]*frac
		} else if idx < n {
			result[i] = data[idx]
		}
	}

	return result
}
