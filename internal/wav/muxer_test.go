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

func TestEncode_ID3ChunkPresentAfterFmt(t *testing.T) {
	samples := make([]int16, 100)
	out, err := wav.Encode(samples, stereoOpts)
	require.NoError(t, err)

	// fmt chunk occupies bytes 12–35 (8-byte header + 16-byte data).
	// id3 chunk must immediately follow.
	assert.Equal(t, []byte("id3 "), out[36:40], "id3 chunk must follow fmt chunk")
}

func TestEncode_DataChunkFollowsID3(t *testing.T) {
	samples := make([]int16, 100)
	opts := wav.Options{SampleRate: 44100, Channels: 2}
	out, err := wav.Encode(samples, opts)
	require.NoError(t, err)

	// Parse id3 chunk size to find where the data chunk starts.
	id3Size := int(binary.LittleEndian.Uint32(out[40:44]))
	id3PaddedSize := id3Size
	if id3PaddedSize%2 != 0 {
		id3PaddedSize++
	}
	dataChunkOffset := 36 + 8 + id3PaddedSize

	require.Less(t, dataChunkOffset+4, len(out), "output too short to contain data chunk")
	assert.Equal(t, []byte("data"), out[dataChunkOffset:dataChunkOffset+4])
}

func TestEncode_PCMSamplesAreCorrect(t *testing.T) {
	// Encode a known waveform and verify the bytes appear verbatim in the output.
	samples := []int16{0x1234, -1, 0x7FFF}
	opts := wav.Options{SampleRate: 44100, Channels: 1}
	out, err := wav.Encode(samples, opts)
	require.NoError(t, err)

	// Find the data chunk offset.
	id3Size := int(binary.LittleEndian.Uint32(out[40:44]))
	id3PaddedSize := id3Size
	if id3PaddedSize%2 != 0 {
		id3PaddedSize++
	}
	pcmOffset := 36 + 8 + id3PaddedSize + 8 // skip data chunk header too

	require.LessOrEqual(t, pcmOffset+6, len(out))
	assert.Equal(t, byte(0x34), out[pcmOffset+0]) // 0x1234 low byte
	assert.Equal(t, byte(0x12), out[pcmOffset+1]) // 0x1234 high byte
	assert.Equal(t, byte(0xFF), out[pcmOffset+2]) // -1 = 0xFFFF low byte
	assert.Equal(t, byte(0xFF), out[pcmOffset+3]) // -1 = 0xFFFF high byte
	assert.Equal(t, byte(0xFF), out[pcmOffset+4]) // 0x7FFF low byte
	assert.Equal(t, byte(0x7F), out[pcmOffset+5]) // 0x7FFF high byte
}

func TestEncode_ID3TagContainsExpectedFrames(t *testing.T) {
	samples := make([]int16, 0)
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Flash Man", Artist: "Tateishi", Album: "Mega Man 2", Track: 5, Year: "1988"},
	}
	out, err := wav.Encode(samples, opts)
	require.NoError(t, err)

	// Extract the raw ID3 tag bytes from the id3 RIFF chunk.
	id3Size := int(binary.LittleEndian.Uint32(out[40:44]))
	id3Bytes := out[44 : 44+id3Size]

	// ID3v2 header: "ID3" + version byte 0x03 (v2.3)
	assert.Equal(t, []byte("ID3"), id3Bytes[0:3])
	assert.Equal(t, byte(0x03), id3Bytes[3], "must be ID3v2.3")

	// The raw bytes must contain the expected frame IDs and text values.
	assert.Contains(t, string(id3Bytes), "TIT2")
	assert.Contains(t, string(id3Bytes), "Flash Man")
	assert.Contains(t, string(id3Bytes), "TPE1")
	assert.Contains(t, string(id3Bytes), "Tateishi")
	assert.Contains(t, string(id3Bytes), "TALB")
	assert.Contains(t, string(id3Bytes), "Mega Man 2")
	assert.Contains(t, string(id3Bytes), "TRCK")
	assert.Contains(t, string(id3Bytes), "5")
	assert.Contains(t, string(id3Bytes), "TYER")
	assert.Contains(t, string(id3Bytes), "1988")
}

func TestEstimatedSize_WithMetadata(t *testing.T) {
	// EstimatedSize must also be exact when metadata is non-empty.
	const durationMs = 5_000
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Guts Man", Artist: "Manami Matsumae", Album: "Mega Man", Track: 1},
	}
	sampleCount := (durationMs * opts.SampleRate / 1000) * opts.Channels
	actual, err := wav.Encode(make([]int16, sampleCount), opts)
	require.NoError(t, err)

	assert.Equal(t, int64(len(actual)), wav.EstimatedSize(durationMs, opts))
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

func TestHeaderBytes_IsExactPrefixOfEncode(t *testing.T) {
	// HeaderBytes must produce bytes that are byte-for-byte identical to the
	// same prefix in Encode. TrackFile.Read serves HeaderBytes for header-only
	// reads; if they diverge, metadata readers see different data than PCM readers.
	const durationMs = 5_000
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata: wav.Metadata{
			Title:  "Flash Man",
			Artist: "Takashi Tateishi",
			Album:  "Mega Man 2",
			Track:  3,
		},
	}

	sampleCount := (durationMs * opts.SampleRate / 1000) * opts.Channels
	samples := make([]int16, sampleCount)
	full, err := wav.Encode(samples, opts)
	require.NoError(t, err)

	header := wav.HeaderBytes(durationMs, opts)

	// Header must be a proper prefix of the full WAV.
	require.Less(t, len(header), len(full), "header must be shorter than full WAV")
	assert.Equal(t, full[:len(header)], header,
		"HeaderBytes must be byte-for-byte identical to the corresponding prefix of Encode")

	// The byte immediately after the header is the first PCM sample byte.
	assert.Equal(t, int64(len(header)), wav.EstimatedSize(durationMs, opts)-int64(sampleCount*2),
		"header length must equal EstimatedSize minus PCM bytes")
}
