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
	out := wav.Encode(samples, stereoOpts)

	assert.Equal(t, []byte("RIFF"), out[0:4], "file must start with RIFF")
	assert.Equal(t, []byte("WAVE"), out[8:12], "RIFF type must be WAVE")
}

func TestEncode_FmtChunkIsCorrect(t *testing.T) {
	samples := make([]int16, 100)
	out := wav.Encode(samples, stereoOpts)

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
	out := wav.Encode(samples, stereoOpts)

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
	out := wav.Encode(samples, opts)
	assert.Greater(t, len(out), 44, "output with metadata must be larger than bare WAV header")
}

func TestEncode_ID3ChunkPresentAfterFmt(t *testing.T) {
	samples := make([]int16, 100)
	out := wav.Encode(samples, stereoOpts)

	// fmt chunk occupies bytes 12–35 (8-byte header + 16-byte data).
	// id3 chunk must immediately follow.
	assert.Equal(t, []byte("id3 "), out[36:40], "id3 chunk must follow fmt chunk")
}

func TestEncode_DataChunkPresent(t *testing.T) {
	samples := make([]int16, 100)
	opts := wav.Options{SampleRate: 44100, Channels: 2}
	out := wav.Encode(samples, opts)

	offset, _ := findChunk(out, "data")
	require.NotEqual(t, -1, offset, "data chunk must be present")
	assert.Equal(t, []byte("data"), out[offset:offset+4])
}

func TestEncode_PCMSamplesAreCorrect(t *testing.T) {
	// Encode a known waveform and verify the bytes appear verbatim in the output.
	samples := []int16{0x1234, -1, 0x7FFF}
	opts := wav.Options{SampleRate: 44100, Channels: 1}
	out := wav.Encode(samples, opts)

	// Find the data chunk and skip its 8-byte header to reach PCM samples.
	dataOffset, _ := findChunk(out, "data")
	require.NotEqual(t, -1, dataOffset, "data chunk must be present")
	pcmOffset := dataOffset + 8

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
	out := wav.Encode(samples, opts)

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

// TestEstimatedSize_MatchesEncode enforces the invariant FUSE getattr relies
// on: the size reported before rendering equals the rendered file's size, and
// the header served before rendering is a prefix of the rendered file.
func TestEstimatedSize_MatchesEncode(t *testing.T) {
	cases := map[string]wav.Metadata{
		"no metadata":                   {},
		"full metadata":                 {Title: "Frog's Theme", Artist: "Uematsu", AlbumArtist: "Mitsuda", Album: "Chrono Trigger", Track: 1},
		"odd-length id3 tag (pad byte)": {Title: "xy"},
	}
	for name, meta := range cases {
		const durationMs = 2_000
		opts := wav.Options{SampleRate: 44100, Channels: 2, Metadata: meta}
		out := wav.Encode(make([]int16, wav.SampleCount(durationMs, opts)), opts)

		assert.Equal(t, int64(len(out)), wav.EstimatedSize(durationMs, opts), name)
		header := wav.HeaderBytes(durationMs, opts)
		assert.Equal(t, out[:len(header)], header, name)
	}
}

// findChunk scans a RIFF WAVE file for a top-level chunk with the given 4-byte
// ID and returns its offset (pointing at the 4-byte ID, not the data) and size.
// Returns -1 if not found.
func findChunk(data []byte, id string) (offset, size int) {
	pos := 12 // skip RIFF+size+WAVE
	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		if chunkID == id {
			return pos, chunkSize
		}
		pos += 8 + chunkSize
		if chunkSize%2 != 0 {
			pos++ // skip pad byte
		}
	}
	return -1, 0
}

func TestEncode_ListInfoChunkPresent(t *testing.T) {
	samples := make([]int16, 100)
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Flash Man", Artist: "Tateishi", Album: "Mega Man 2"},
	}
	out := wav.Encode(samples, opts)

	offset, size := findChunk(out, "LIST")
	require.NotEqual(t, -1, offset, "LIST chunk must be present")
	require.GreaterOrEqual(t, size, 4, "LIST chunk must contain at least INFO marker")
	require.LessOrEqual(t, offset+8+size, len(out))

	// LIST chunk data starts with "INFO"
	assert.Equal(t, []byte("INFO"), out[offset+8:offset+12])

	// Subchunk content must contain the metadata fields
	listContent := string(out[offset+8 : offset+8+size])
	assert.Contains(t, listContent, "INAM")
	assert.Contains(t, listContent, "Flash Man")
	assert.Contains(t, listContent, "IART")
	assert.Contains(t, listContent, "Tateishi")
	assert.Contains(t, listContent, "IPRD")
	assert.Contains(t, listContent, "Mega Man 2")
}

func TestEncode_ListInfoChunk_EmptyMetadata(t *testing.T) {
	// When no metadata fields are set, no LIST chunk should be emitted.
	samples := make([]int16, 100)
	opts := wav.Options{SampleRate: 44100, Channels: 2} // no Metadata
	out := wav.Encode(samples, opts)

	offset, _ := findChunk(out, "LIST")
	assert.Equal(t, -1, offset, "LIST chunk must be absent when metadata is empty")
}

func TestEncode_AlbumArtistWritesTPE2(t *testing.T) {
	out := wav.Encode(nil, wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Frog's Theme", Artist: "Uematsu", AlbumArtist: "Mitsuda", Album: "Chrono Trigger", Track: 1},
	})

	id3Size := int(binary.LittleEndian.Uint32(out[40:44]))
	id3Bytes := string(out[44 : 44+id3Size])
	assert.Contains(t, id3Bytes, "TPE2")
	assert.Contains(t, id3Bytes, "Mitsuda")
	assert.Contains(t, id3Bytes, "TPE1", "track artist must remain")
	assert.Contains(t, id3Bytes, "Uematsu")
}

func TestEncode_EmptyAlbumArtistOmitsTPE2(t *testing.T) {
	out := wav.Encode(nil, wav.Options{SampleRate: 44100, Channels: 2, Metadata: wav.Metadata{Title: "x", Artist: "y"}})
	assert.NotContains(t, string(out), "TPE2")
}
