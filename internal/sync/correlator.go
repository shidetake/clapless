package sync

import (
	"fmt"
	"math"
	"math/cmplx"

	"gonum.org/v1/gonum/dsp/fourier"
)

// OffsetResult contains the detected offset and confidence score
type OffsetResult struct {
	OffsetSamples int     // Offset in samples (positive = local needs to shift later/right = local is ahead/early)
	OffsetSeconds float64 // Offset in seconds
	Confidence    float64 // Confidence score (0.0 to 1.0)
}

// DetectOffset finds the time offset between mixed and local audio using cross-correlation
func DetectOffset(mixed, local []float64, sampleRate, segmentDuration, downsampleFactor int) (*OffsetResult, error) {

	// Validate input data
	if len(mixed) == 0 {
		return nil, fmt.Errorf("mixed audio data is empty")
	}
	if len(local) == 0 {
		return nil, fmt.Errorf("local audio data is empty")
	}

	// Coarse search with downsampling
	mixedCoarse := downsample(mixed, downsampleFactor)
	localCoarse := downsample(local, downsampleFactor)

	// Normalize entire signals
	mixedNorm := normalize(mixedCoarse)
	localNorm := normalize(localCoarse)

	// Compute cross-correlation using FFT
	correlation := crossCorrelateFFT(mixedNorm, localNorm)

	// Find peak
	peakIdx, peakValue := findMaxPeak(correlation)

	// Calculate offset from peak position
	// FFT correlation: result[k] means local should be shifted k samples to the right
	// So offset = peak_index directly
	offset := peakIdx

	// Convert to original sample rate
	finalOffset := offset * downsampleFactor

	// Calculate confidence (normalized correlation peak)
	confidence := peakValue / float64(len(localNorm))

	return &OffsetResult{
		OffsetSamples: finalOffset,
		OffsetSeconds: float64(finalOffset) / float64(sampleRate),
		Confidence:    confidence,
	}, nil
}

// normalize scales audio data to have zero mean and unit variance
func normalize(data []float64) []float64 {
	if len(data) == 0 {
		return data
	}

	// Calculate mean
	mean := 0.0
	for _, v := range data {
		mean += v
	}
	mean /= float64(len(data))

	// Calculate standard deviation
	variance := 0.0
	for _, v := range data {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(data))
	stdDev := math.Sqrt(variance)

	// Avoid division by zero
	if stdDev == 0 {
		stdDev = 1.0
	}

	// Normalize
	result := make([]float64, len(data))
	for i, v := range data {
		result[i] = (v - mean) / stdDev
	}

	return result
}


// crossCorrelateFFT performs FFT-based cross-correlation
// Returns correlation array where peak indicates best alignment
func crossCorrelateFFT(signal1, signal2 []float64) []float64 {
	// Validate inputs (defensive check)
	if len(signal1) == 0 || len(signal2) == 0 {
		return []float64{0}
	}

	n := len(signal1) + len(signal2) - 1
	fftSize := nextPowerOfTwo(n)

	// Pad signals to FFT size
	padded1 := padToSize(signal1, fftSize)
	padded2 := padToSize(signal2, fftSize)

	// Create FFT
	fft := fourier.NewFFT(fftSize)

	// Forward FFT (real input to complex output)
	fft1 := fft.Coefficients(nil, padded1)
	fft2 := fft.Coefficients(nil, padded2)

	// Multiply in frequency domain: FFT1 * conj(FFT2)
	product := make([]complex128, len(fft1))
	for i := range product {
		product[i] = fft1[i] * cmplx.Conj(fft2[i])
	}

	// Inverse FFT (complex input to real output)
	resultReal := fft.Sequence(nil, product)

	// Gonum FFT is unnormalized - need to divide by fftSize
	// (Coefficients followed by Sequence multiplies by length)
	for i := range resultReal {
		resultReal[i] /= float64(fftSize)
	}

	// Trim to actual correlation size
	result := make([]float64, n)
	copy(result, resultReal[:n])

	return result
}

// findMaxPeak finds the index and value of the maximum peak in the correlation
func findMaxPeak(correlation []float64) (int, float64) {
	if len(correlation) == 0 {
		return 0, 0
	}

	maxIdx := 0
	maxVal := correlation[0]

	for i, v := range correlation {
		if v > maxVal {
			maxVal = v
			maxIdx = i
		}
	}

	return maxIdx, maxVal
}

// nextPowerOfTwo returns the next power of 2 >= n
func nextPowerOfTwo(n int) int {
	power := 1
	for power < n {
		power *= 2
	}
	return power
}

// padToSize pads a slice with zeros to reach the target size
func padToSize(data []float64, size int) []float64 {
	if len(data) >= size {
		return data
	}

	result := make([]float64, size)
	copy(result, data)
	return result
}

// downsample reduces the sample rate by taking every Nth sample
func downsample(data []float64, factor int) []float64 {
	if factor <= 1 {
		return data
	}

	result := make([]float64, 0, len(data)/factor)
	for i := 0; i < len(data); i += factor {
		result = append(result, data[i])
	}
	return result
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
