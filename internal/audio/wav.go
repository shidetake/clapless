package audio

import (
	"fmt"
	"os"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// WAVData represents WAV file metadata and audio data
type WAVData struct {
	Path       string
	SampleRate int
	Channels   int
	BitDepth   int
	Data       []float64 // Audio data as float64 samples (normalized to -1.0 to 1.0)
	Format     *audio.Format
}

// LoadWAV reads a WAV file and returns its data
func LoadWAV(path string) (*WAVData, error) {
	// Open WAV file
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAV file %s: %w", path, err)
	}
	defer f.Close()

	// Decode WAV
	decoder := wav.NewDecoder(f)
	if !decoder.IsValidFile() {
		return nil, fmt.Errorf("invalid WAV file: %s", path)
	}

	// Read format information
	format := decoder.Format()
	sampleRate := int(decoder.SampleRate)
	channels := int(decoder.NumChans)
	bitDepth := int(decoder.BitDepth)

	// Read all audio data in chunks
	const bufferSize = 4096
	allData := make([]int, 0)

	for {
		buf := &audio.IntBuffer{
			Data:   make([]int, bufferSize),
			Format: format,
		}

		n, err := decoder.PCMBuffer(buf)
		if err != nil {
			return nil, fmt.Errorf("failed to read PCM data from %s: %w", path, err)
		}
		if n == 0 {
			break
		}

		// Append read data
		allData = append(allData, buf.Data[:n]...)
	}

	// Check if file contains any audio data
	if len(allData) == 0 {
		return nil, fmt.Errorf("WAV file contains no audio data: %s", path)
	}

	// Convert int samples to float64 (normalized to -1.0 to 1.0)
	data := make([]float64, len(allData))
	maxVal := 1 << uint(bitDepth-1)
	for i, sample := range allData {
		data[i] = float64(sample) / float64(maxVal)
	}

	return &WAVData{
		Path:       path,
		SampleRate: sampleRate,
		Channels:   channels,
		BitDepth:   bitDepth,
		Data:       data,
		Format:     format,
	}, nil
}

// WriteWAV writes audio data to a WAV file
func WriteWAV(path string, data []float64, sampleRate, channels, bitDepth int) error {
	// Create output file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create WAV file %s: %w", path, err)
	}
	defer f.Close()

	// Create encoder
	encoder := wav.NewEncoder(f, sampleRate, bitDepth, channels, 1)
	defer encoder.Close()

	// Convert float64 samples back to int
	maxVal := 1 << uint(bitDepth-1)
	intData := make([]int, len(data))
	for i, sample := range data {
		// Clamp to [-1.0, 1.0] range
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		intData[i] = int(sample * float64(maxVal))
	}

	// Create buffer
	buf := &audio.IntBuffer{
		Data: intData,
		Format: &audio.Format{
			NumChannels: channels,
			SampleRate:  sampleRate,
		},
	}

	// Write to file
	if err := encoder.Write(buf); err != nil {
		return fmt.Errorf("failed to write WAV data to %s: %w", path, err)
	}

	return nil
}

// ToMono converts stereo (or multi-channel) audio to mono by averaging channels
func ToMono(data []float64, channels int) []float64 {
	if channels == 1 {
		return data
	}

	numSamples := len(data) / channels
	mono := make([]float64, numSamples)

	for i := 0; i < numSamples; i++ {
		sum := 0.0
		for ch := 0; ch < channels; ch++ {
			sum += data[i*channels+ch]
		}
		mono[i] = sum / float64(channels)
	}

	return mono
}

// Duration returns the duration of the audio in seconds
func (w *WAVData) Duration() float64 {
	totalSamples := len(w.Data) / w.Channels
	return float64(totalSamples) / float64(w.SampleRate)
}

// DurationString returns a human-readable duration string (MM:SS format)
func (w *WAVData) DurationString() string {
	duration := w.Duration()
	minutes := int(duration) / 60
	seconds := int(duration) % 60
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
