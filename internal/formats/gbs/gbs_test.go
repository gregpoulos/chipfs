package gbs_test

import (
	"testing"

	"github.com/gregpoulos/chipfs/internal/formats/gbs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeGBS constructs a minimal syntactically valid GBS binary for use in tests.
func makeGBS(trackCount int, title, author, copyright string) []byte {
	buf := make([]byte, 0x70)
	copy(buf[0:3], "GBS")
	buf[3] = 0x01          // version 1
	buf[4] = byte(trackCount)
	buf[5] = 0x01          // first track (1-indexed)
	// load/init/play addresses and stack pointer: zeroed (valid for tests)
	copyNullPadded(buf[0x10:0x30], title)
	copyNullPadded(buf[0x30:0x50], author)
	copyNullPadded(buf[0x50:0x70], copyright)
	return buf
}

func copyNullPadded(dst []byte, s string) {
	b := []byte(s)
	if len(b) > len(dst) {
		b = b[:len(dst)]
	}
	copy(dst, b)
}

func TestParse_ValidGBS(t *testing.T) {
	data := makeGBS(12, "Pokemon Red", "Junichi Masuda", "1996 Nintendo")

	h, err := gbs.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, 12, h.TrackCount)
	assert.Equal(t, 1, h.FirstTrack)
	assert.Equal(t, "Pokemon Red", h.Title)
	assert.Equal(t, "Junichi Masuda", h.Author)
	assert.Equal(t, "1996 Nintendo", h.Copyright)
}

func TestParse_RejectsInvalidMagic(t *testing.T) {
	_, err := gbs.Parse([]byte("NOTGBS\x01"))
	assert.ErrorIs(t, err, gbs.ErrInvalidMagic)
}

func TestParse_RejectsTooShort(t *testing.T) {
	_, err := gbs.Parse([]byte("GBS"))
	assert.Error(t, err)
}

func TestParse_NullPaddedStringsAreTrimmed(t *testing.T) {
	data := makeGBS(1, "Tetris", "Hirokazu Tanaka", "1989 Nintendo")
	h, err := gbs.Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "Tetris", h.Title)
	assert.Equal(t, "Hirokazu Tanaka", h.Author)
}
