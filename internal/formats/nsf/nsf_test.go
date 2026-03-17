package nsf_test

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/gregpoulos/chipfs/internal/formats/nsf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeNSF constructs a minimal syntactically valid NSF binary for use in tests.
// Fields not specified are zeroed (valid for a minimal NSF header).
func makeNSF(trackCount int, title, artist, copyright string) []byte {
	buf := make([]byte, 128)
	copy(buf[0:5], []byte{0x4E, 0x45, 0x53, 0x4D, 0x1A}) // "NESM\x1A"
	buf[5] = 0x01                                          // version 1
	buf[6] = byte(trackCount)
	buf[7] = 0x01 // first track (1-indexed)
	// load/init/play addresses: set to valid-looking values
	buf[8], buf[9] = 0x00, 0x80   // load addr  = 0x8000
	buf[10], buf[11] = 0x00, 0x80 // init addr  = 0x8000
	buf[12], buf[13] = 0x05, 0x80 // play addr  = 0x8005
	copyNullPadded(buf[14:46], title)
	copyNullPadded(buf[46:78], artist)
	copyNullPadded(buf[78:110], copyright)
	return buf
}

func copyNullPadded(dst []byte, s string) {
	b := []byte(s)
	if len(b) > len(dst) {
		b = b[:len(dst)]
	}
	copy(dst, b)
	// remainder is already zero from make()
}

func TestParse_ValidNSF(t *testing.T) {
	data := makeNSF(3, "Mega Man 2", "Takashi Tateishi", "1988 Capcom")

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, 3, h.TrackCount)
	assert.Equal(t, 1, h.FirstTrack)
	assert.Equal(t, "Mega Man 2", h.Title)
	assert.Equal(t, "Takashi Tateishi", h.Artist)
	assert.Equal(t, "1988 Capcom", h.Copyright)
}

func TestParse_RejectsInvalidMagic(t *testing.T) {
	_, err := nsf.Parse([]byte("NOT AN NSF FILE AT ALL"))
	assert.ErrorIs(t, err, nsf.ErrInvalidMagic)
}

func TestParse_RejectsTooShort(t *testing.T) {
	// A valid header is 128 bytes; anything shorter should be rejected
	_, err := nsf.Parse([]byte{0x4E, 0x45, 0x53, 0x4D, 0x1A})
	assert.Error(t, err)
}

func TestParse_TrackCountRange(t *testing.T) {
	tests := []struct {
		name       string
		trackCount int
	}{
		{"one track", 1},
		{"typical game", 24},
		{"max tracks", 255},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := makeNSF(tt.trackCount, "Game", "Artist", "2024")
			h, err := nsf.Parse(data)
			require.NoError(t, err)
			assert.Equal(t, tt.trackCount, h.TrackCount)
		})
	}
}

func TestParse_NullPaddedStringsAreTrimmed(t *testing.T) {
	// NSF strings are null-padded to exactly 32 bytes; trailing nulls must be stripped
	data := makeNSF(1, "Short Title", "Artist", "2024")
	h, err := nsf.Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "Short Title", h.Title, "trailing null bytes should not appear in result")
}

// TestParse_SMB tests against a real NSF file (Super Mario Bros.) to catch
// assumptions that synthetic fixtures might not exercise.
func TestParse_SMB(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/smb.nsf")
	require.NoError(t, err, "testdata/fixtures/smb.nsf must exist; copy it there to run this test")

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, 18, h.TrackCount)
	assert.Equal(t, 1, h.FirstTrack)
	assert.Equal(t, "Super Mario Bros.", h.Title)
	assert.Equal(t, "Koji Kondo", h.Artist)
	assert.Equal(t, "1985 Nintendo", h.Copyright)
}

// --- NSFe helpers ---

// nsfeChunk holds a single NSFe chunk's ID and data.
type nsfeChunk struct {
	id   string // exactly 4 bytes
	data []byte
}

// makeNSFe builds a minimal NSFe binary from the provided chunks.
// The caller is responsible for including a terminating NEND chunk if desired.
func makeNSFe(chunks []nsfeChunk) []byte {
	var buf []byte
	buf = append(buf, []byte("NSFE")...)
	for _, c := range chunks {
		size := uint32(len(c.data))
		buf = append(buf, byte(size), byte(size>>8), byte(size>>16), byte(size>>24))
		buf = append(buf, []byte(c.id)...)
		buf = append(buf, c.data...)
	}
	return buf
}

// infoChunk builds an NSFe INFO chunk with the given track count and first
// track index (0-indexed, as stored in the file).
func infoChunk(trackCount, firstTrack int) nsfeChunk {
	data := make([]byte, 10)
	data[0], data[1] = 0x00, 0x80 // load addr 0x8000
	data[2], data[3] = 0x00, 0x80 // init addr 0x8000
	data[4], data[5] = 0x05, 0x80 // play addr 0x8005
	data[6] = 0x00                 // NTSC
	data[7] = 0x00                 // no extra sound chips
	data[8] = byte(trackCount)
	data[9] = byte(firstTrack)
	return nsfeChunk{"INFO", data}
}

// authChunk builds an NSFe auth chunk from four null-terminated strings.
func authChunk(game, artist, copyright, ripper string) nsfeChunk {
	var data []byte
	for _, s := range []string{game, artist, copyright, ripper} {
		data = append(data, []byte(s)...)
		data = append(data, 0x00)
	}
	return nsfeChunk{"auth", data}
}

// tlblChunk builds an NSFe tlbl chunk from a slice of track title strings.
func tlblChunk(titles []string) nsfeChunk {
	var data []byte
	for _, t := range titles {
		data = append(data, []byte(t)...)
		data = append(data, 0x00)
	}
	return nsfeChunk{"tlbl", data}
}

// timeChunk builds an NSFe time chunk from a slice of durations in milliseconds.
func timeChunk(durationsMs []int) nsfeChunk {
	data := make([]byte, 4*len(durationsMs))
	for i, d := range durationsMs {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(d))
	}
	return nsfeChunk{"time", data}
}

// fadeChunk builds an NSFe fade chunk from a slice of fade durations in milliseconds.
func fadeChunk(fadeDurationsMs []int) nsfeChunk {
	data := make([]byte, 4*len(fadeDurationsMs))
	for i, d := range fadeDurationsMs {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(d))
	}
	return nsfeChunk{"fade", data}
}

var nend = nsfeChunk{"NEND", nil}

// --- NSFe tests ---

func TestParseNSFe_ValidMinimal(t *testing.T) {
	// INFO + NEND only: no auth, tlbl, time, or fade chunks
	data := makeNSFe([]nsfeChunk{infoChunk(3, 0), nend})

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, 3, h.TrackCount)
	assert.Equal(t, 1, h.FirstTrack) // 0-indexed in file → 1-indexed in Header
	assert.Empty(t, h.Title)
	assert.Empty(t, h.Artist)
	assert.Empty(t, h.Tracks)
}

func TestParseNSFe_WithAuth(t *testing.T) {
	data := makeNSFe([]nsfeChunk{
		infoChunk(2, 0),
		authChunk("Mega Man 2", "Manami Matsumae", "1988 Capcom", ""),
		nend,
	})

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "Mega Man 2", h.Title)
	assert.Equal(t, "Manami Matsumae", h.Artist)
	assert.Equal(t, "1988 Capcom", h.Copyright)
}

func TestParseNSFe_WithTrackLabels(t *testing.T) {
	titles := []string{"Title Screen", "Stage 1", "Boss"}
	data := makeNSFe([]nsfeChunk{
		infoChunk(3, 0),
		tlblChunk(titles),
		nend,
	})

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	require.Len(t, h.Tracks, 3)
	assert.Equal(t, "Title Screen", h.Tracks[0].Title)
	assert.Equal(t, "Stage 1", h.Tracks[1].Title)
	assert.Equal(t, "Boss", h.Tracks[2].Title)
}

func TestParseNSFe_WithDurations(t *testing.T) {
	data := makeNSFe([]nsfeChunk{
		infoChunk(2, 0),
		timeChunk([]int{180_000, 90_000}),
		fadeChunk([]int{10_000, 5_000}),
		nend,
	})

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	require.Len(t, h.Tracks, 2)
	assert.Equal(t, 180_000, h.Tracks[0].DurationMs)
	assert.Equal(t, 10_000, h.Tracks[0].FadeMs)
	assert.Equal(t, 90_000, h.Tracks[1].DurationMs)
	assert.Equal(t, 5_000, h.Tracks[1].FadeMs)
}

func TestParseNSFe_SkipsUnknownChunks(t *testing.T) {
	// An unknown chunk ("BANK") between INFO and auth must be skipped gracefully.
	data := makeNSFe([]nsfeChunk{
		infoChunk(1, 0),
		{"BANK", []byte{0x00, 0x01, 0x02, 0x03}},
		authChunk("DuckTales", "Composer", "1989", ""),
		nend,
	})

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "DuckTales", h.Title)
}

func TestParseNSFe_RejectsInvalidMagic(t *testing.T) {
	_, err := nsf.Parse([]byte("NOT AN NSFE FILE AT ALL"))
	assert.ErrorIs(t, err, nsf.ErrInvalidMagic)
}

func TestParseNSFe_MissingINFO(t *testing.T) {
	// A file with NSFE magic but no INFO chunk must be rejected.
	data := makeNSFe([]nsfeChunk{
		authChunk("Game", "Artist", "2024", ""),
		nend,
	})

	_, err := nsf.Parse(data)
	assert.Error(t, err)
}

func TestParseNSFe_TruncatedINFO(t *testing.T) {
	// INFO chunk data must be at least 8 bytes.
	data := makeNSFe([]nsfeChunk{
		{"INFO", []byte{0x00, 0x80, 0x00, 0x80, 0x05}}, // only 5 bytes
		nend,
	})

	_, err := nsf.Parse(data)
	assert.Error(t, err)
}

// TestParse_DuckTales tests against a real NSFe file to catch assumptions
// that synthetic fixtures might not exercise.
func TestParse_DuckTales(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/ducktales.nsfe")
	require.NoError(t, err, "testdata/fixtures/ducktales.nsfe must exist")

	h, err := nsf.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, 45, h.TrackCount)
	assert.Equal(t, 1, h.FirstTrack)
	assert.Equal(t, "DuckTales", h.Title)
	assert.Equal(t, "Hiroshige Tonomura, Yoshihiro Sakaguchi", h.Artist)
	assert.Equal(t, "\xa91989 Capcom", h.Copyright)

	require.Greater(t, len(h.Tracks), 0)
	assert.Equal(t, "Title Screen / Ending - Part 2 [DuckTales Theme]", h.Tracks[0].Title)
	assert.Equal(t, 105_000, h.Tracks[0].DurationMs)
	assert.Equal(t, 10_000, h.Tracks[0].FadeMs)
}
