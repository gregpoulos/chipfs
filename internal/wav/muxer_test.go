package wav_test

import (
	"encoding/binary"
	"testing"

	"github.com/gregpoulos/chipfs/internal/wav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var stereoOpts = wav.Options{SampleRate: 44100, Channels: 2}

func TestEncode_StartsWithRIFFHeader(t *testing.T) {
	samples := make([]int16, 100)
	out, err := wav.Encode(samples, stereoOpts)
	require.NoError(t, err)

	assert.Equal(t, []byte("RIFF"), out[0:4], "file must start with RIFF")
	assert.Equal(t, []byte("WAVE"), out[8:12], "RIFF type must be WAVE")
}

func TestEncode_FmtChunkIsCorrect(t *testing.T) {
	samples := make([]int16, 100)
	out, err := wav.Encode(samples, stereoOpts)
	require.NoError(t, err)

	assert.Equal(t, []byte("fmt "), out[12:16])

	fmtSize := binary.LittleEndian.Uint32(out[16:20])
	assert.Equal(t, uint32(16), fmtSize, "PCM fmt chunk is always 16 bytes")

	audioFmt := binary.LittleEndian.Uint16(out[20:22])
	assert.Equal(t, uint16(1), audioFmt, "audio format 1 = PCM")

	channels := binary.LittleEndian.Uint16(out[22:24])
	assert.Equal(t, uint16(2), channels)

	sampleRate := binary.LittleEndian.Uint32(out[24:28])
	assert.Equal(t, uint32(44100), sampleRate)

	bitsPerSample := binary.LittleEndian.Uint16(out[34:36])
	assert.Equal(t, uint16(16), bitsPerSample)
}

func TestEncode_RIFFSizeMatchesActualLength(t *testing.T) {
	samples := make([]int16, 88200) // 1 second stereo
	out, err := wav.Encode(samples, stereoOpts)
	require.NoError(t, err)

	// The RIFF size field at bytes [4:8] must equal len(out) - 8
	riffSize := binary.LittleEndian.Uint32(out[4:8])
	assert.Equal(t, uint32(len(out)-8), riffSize)
}

func TestEncode_WithMetadata(t *testing.T) {
	samples := make([]int16, 100)
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata: wav.Metadata{
			Title:  "Dr. Wily Stage 1",
			Artist: "Takashi Tateishi",
			Album:  "Mega Man 2",
			Track:  3,
		},
	}
	out, err := wav.Encode(samples, opts)
	require.NoError(t, err)
	assert.Greater(t, len(out), 44, "output with metadata must be larger than bare WAV header")
}

func TestEstimatedSize_MatchesActualEncodeOutput(t *testing.T) {
	// EstimatedSize must predict the exact byte length that Encode produces.
	// This invariant is critical: FUSE getattr uses EstimatedSize before emulation starts,
	// and a mismatch causes media servers to truncate or reject the stream.
	const durationMs = 10_000 // 10 seconds
	opts := wav.Options{SampleRate: 44100, Channels: 2}

	sampleCount := (durationMs * opts.SampleRate / 1000) * opts.Channels
	samples := make([]int16, sampleCount)

	actual, err := wav.Encode(samples, opts)
	require.NoError(t, err)

	estimated := wav.EstimatedSize(durationMs, opts)
	assert.Equal(t, int64(len(actual)), estimated,
		"EstimatedSize must exactly predict the output of Encode for the same duration")
}
