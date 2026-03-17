package spc_test

import (
	"fmt"
	"os"
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

// makeSPCBinary constructs a minimal SPC binary using the ID666 binary format.
// Durations use raw LE integers; artist is at 0xB0.
func makeSPCBinary(song, game, artist string, playSeconds, fadeDurationMs int) []byte {
	buf := make([]byte, 0x200)
	copy(buf[0:33], "SNES-SPC700 Sound File Data v0.30")
	buf[33] = 0x1A
	buf[34] = 0x1A
	buf[35] = 26 // ID666 tag present

	copyNullPadded(buf[0x2E:0x4E], song)
	copyNullPadded(buf[0x4E:0x6E], game)
	copyNullPadded(buf[0xB0:0xD0], artist)

	// Date in binary format: day/month at 0x9E/0x9F, year at 0xA0 (LE uint16).
	// Use a non-slash year byte to ensure the text-format heuristic fires correctly.
	buf[0x9E] = 1    // day
	buf[0x9F] = 1    // month
	buf[0xA0] = 0xD0 // year low byte (non-ASCII, confirms binary)
	buf[0xA1] = 0x07 // year high byte (= 2000)

	// Play duration: 24-bit LE integer at 0xA9.
	buf[0xA9] = byte(playSeconds)
	buf[0xAA] = byte(playSeconds >> 8)
	buf[0xAB] = byte(playSeconds >> 16)

	// Fade duration: 32-bit LE integer at 0xAC.
	buf[0xAC] = byte(fadeDurationMs)
	buf[0xAD] = byte(fadeDurationMs >> 8)
	buf[0xAE] = byte(fadeDurationMs >> 16)
	buf[0xAF] = byte(fadeDurationMs >> 24)

	return buf
}

func TestParse_BinaryFormat_Basic(t *testing.T) {
	data := makeSPCBinary("Frog's Theme", "Chrono Trigger", "Yasunori Mitsuda", 61, 8000)

	h, err := spc.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "Frog's Theme", h.SongTitle)
	assert.Equal(t, "Chrono Trigger", h.GameTitle)
	assert.Equal(t, "Yasunori Mitsuda", h.Artist)
	assert.Equal(t, 61_000, h.PlayDurationMs)
	assert.Equal(t, 8_000, h.FadeDurationMs)
}

func TestParse_BinaryFormat_LargeDuration(t *testing.T) {
	// 300 seconds = 0x12C, spans two bytes; fade = 10000 ms.
	data := makeSPCBinary("Song", "Game", "Artist", 300, 10_000)

	h, err := spc.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, 300_000, h.PlayDurationMs)
	assert.Equal(t, 10_000, h.FadeDurationMs)
}

// TestParse_FrogsTheme tests against a real SPC file (Frog's Theme, Chrono Trigger)
// to catch assumptions that synthetic fixtures might not exercise.
func TestParse_FrogsTheme(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/frogs-theme.spc")
	require.NoError(t, err, "testdata/fixtures/frogs-theme.spc must exist")

	h, err := spc.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, "Frog's Theme", h.SongTitle)
	assert.Equal(t, "Chrono Trigger", h.GameTitle)
	assert.Equal(t, "Yasunori Mitsuda", h.Artist)
	assert.Equal(t, 61_000, h.PlayDurationMs)
	assert.Equal(t, 8_000, h.FadeDurationMs)
}
