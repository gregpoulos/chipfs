package spc_test

import (
	"fmt"
	"testing"

	"github.com/gregpoulos/chipfs/internal/formats/spc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSPC constructs a minimal syntactically valid SPC binary for use in tests.
// The file is padded to 0x200 bytes (sufficient to cover all ID666 tag offsets).
func makeSPC(song, game, artist string, playSeconds, fadeDurationMs int) []byte {
	buf := make([]byte, 0x200)
	// Magic: 33-byte ASCII string + 0x1A 0x1A
	copy(buf[0:33], "SNES-SPC700 Sound File Data v0.30")
	buf[33] = 0x1A
	buf[34] = 0x1A
	buf[35] = 26 // ID666 tag present (value >= 26 indicates text format)

	copyNullPadded(buf[0x2E:0x4E], song)
	copyNullPadded(buf[0x4E:0x6E], game)
	copyNullPadded(buf[0xB1:0xD1], artist)

	// Play duration: 3-character ASCII decimal seconds at 0xA9
	dur := fmt.Sprintf("%-3d", playSeconds)
	copy(buf[0xA9:0xAC], dur)

	// Fade duration: 5-character ASCII decimal milliseconds at 0xAC
	fade := fmt.Sprintf("%-5d", fadeDurationMs)
	copy(buf[0xAC:0xB1], fade)

	return buf
}

func copyNullPadded(dst []byte, s string) {
	b := []byte(s)
	if len(b) > len(dst) {
		b = b[:len(dst)]
	}
	copy(dst, b)
}

func TestParse_ValidSPC(t *testing.T) {
	data := makeSPC("Frog's Theme", "Chrono Trigger", "Yasunori Mitsuda", 120, 8000)

	h, err := spc.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "Frog's Theme", h.SongTitle)
	assert.Equal(t, "Chrono Trigger", h.GameTitle)
	assert.Equal(t, "Yasunori Mitsuda", h.Artist)
	assert.Equal(t, 120_000, h.PlayDurationMs, "play duration should be converted from seconds to ms")
	assert.Equal(t, 8000, h.FadeDurationMs)
}

func TestParse_RejectsInvalidMagic(t *testing.T) {
	_, err := spc.Parse([]byte("NOT AN SPC FILE"))
	assert.ErrorIs(t, err, spc.ErrInvalidMagic)
}

func TestParse_RejectsTooShort(t *testing.T) {
	_, err := spc.Parse([]byte("SNES-SPC700"))
	assert.Error(t, err)
}

func TestParse_ZeroPlayDuration(t *testing.T) {
	// A play duration of 0 seconds is valid; consumers should apply a default
	data := makeSPC("Song", "Game", "Artist", 0, 0)
	h, err := spc.Parse(data)
	require.NoError(t, err)
	assert.Equal(t, 0, h.PlayDurationMs)
}
