package gme_test

import (
	"os"
	"testing"

	"github.com/gregpoulos/chipfs/internal/gme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pentlyFixture loads the Pently demo NSF fixture, failing the test if absent.
func pentlyFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/pently.nsf")
	require.NoError(t, err, "testdata/fixtures/pently.nsf must exist")
	return data
}

func TestOpen_RejectsZeroSampleRate(t *testing.T) {
	_, err := gme.Open([]byte("NESM\x1a"), 0)
	assert.ErrorIs(t, err, gme.ErrInvalidSampleRate)
}

func TestOpen_RejectsNegativeSampleRate(t *testing.T) {
	_, err := gme.Open([]byte("NESM\x1a"), -44100)
	assert.ErrorIs(t, err, gme.ErrInvalidSampleRate)
}

func TestOpen_RejectsInvalidData(t *testing.T) {
	_, err := gme.Open([]byte("this is not a music file"), 44100)
	assert.Error(t, err)
}

func TestOpen_Pently(t *testing.T) {
	emu, err := gme.Open(pentlyFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	assert.Equal(t, 24, emu.TrackCount())
}

func TestTrackInfo_Pently(t *testing.T) {
	emu, err := gme.Open(pentlyFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	info, err := emu.TrackInfo(0)
	require.NoError(t, err)

	// NSF stores global metadata; libgme exposes it on every track.
	assert.Equal(t, "Pently demo", info.Game)
	assert.Equal(t, "DJ Tepples", info.Author)
	assert.Equal(t, "2019 Damian Yerrick", info.Copyright)
	// play_length for plain NSF (no per-track duration) defaults to 150000ms (2.5 min).
	assert.Greater(t, info.PlayMs, 0)
	// fade_length is -1 when not specified by the file.
	assert.Equal(t, -1, info.FadeMs)
}

func TestPlay_ProducesNonZeroSamples(t *testing.T) {
	emu, err := gme.Open(pentlyFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	require.NoError(t, emu.StartTrack(0))

	buf := make([]int16, 4096)
	require.NoError(t, emu.Play(buf))

	nonZero := 0
	for _, s := range buf {
		if s != 0 {
			nonZero++
		}
	}
	assert.Greater(t, nonZero, 0, "Play must produce non-silent audio for a real track")
}

// TestTrackCount_PentlyNSFe cross-validates our NSFe parser against libgme.
// pently-demo.nsfe has a plst chunk with 10 entries (songs only, excluding 15 sfx
// tracks); libgme's TrackCount() returns the playlist length (10), not the raw
// internal track count (25). Any code that uses our Go parser's TrackCount must
// match this value.
func TestTrackCount_PentlyNSFe(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/pently-demo.nsfe")
	require.NoError(t, err, "testdata/fixtures/pently-demo.nsfe must exist")

	emu, err := gme.Open(data, 44100)
	require.NoError(t, err)
	defer emu.Close()

	assert.Equal(t, 10, emu.TrackCount(),
		"plst chunk has 10 entries; libgme TrackCount() must reflect playlist length")
}

func TestTrackEnded_AfterFade(t *testing.T) {
	emu, err := gme.Open(pentlyFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	require.NoError(t, emu.StartTrack(0))
	emu.SetFade(0, 500) // start fading immediately, 500ms fade → ends after ~500ms

	buf := make([]int16, 4096) // ~46ms per iteration at 44100Hz stereo
	// 200 iterations ≈ 9.2s of audio. This covers both libgme >= 0.6.4
	// (gme_set_fade_msecs: fade ends at 500ms) and older libgme (gme_set_fade
	// fallback: 8-second default fade ends at ~8s). Emulation runs much
	// faster than real-time so 200 iterations completes in milliseconds.
	for i := 0; i < 200; i++ {
		if emu.TrackEnded() {
			return
		}
		require.NoError(t, emu.Play(buf))
	}
	t.Error("TrackEnded never returned true after fade")
}
