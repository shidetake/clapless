package audio

// GenerateSilence creates silence samples of specified duration
func GenerateSilence(numSamples int) []float64 {
	return make([]float64, numSamples)
}

// PrependSilence adds silence to the beginning of audio data
func PrependSilence(data []float64, silenceSamples int) []float64 {
	if silenceSamples <= 0 {
		return data
	}

	silence := GenerateSilence(silenceSamples)
	result := make([]float64, len(silence)+len(data))
	copy(result, silence)
	copy(result[len(silence):], data)
	return result
}

// SamplesToSeconds converts sample count to seconds
func SamplesToSeconds(samples, sampleRate int) float64 {
	return float64(samples) / float64(sampleRate)
}

// SecondsToSamples converts seconds to sample count
func SecondsToSamples(seconds float64, sampleRate int) int {
	return int(seconds * float64(sampleRate))
}
