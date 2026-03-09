package nsf_test

import (
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
