package gme_test

import (
	"os"
	"testing"

	"github.com/gregpoulos/chipfs/internal/gme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smbFixture loads the Super Mario Bros. NSF fixture, failing the test if absent.
func smbFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/smb.nsf")
	require.NoError(t, err, "testdata/fixtures/smb.nsf must exist")
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

func TestOpen_SMB(t *testing.T) {
	emu, err := gme.Open(smbFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	assert.Equal(t, 18, emu.TrackCount())
}

func TestTrackInfo_SMB(t *testing.T) {
	emu, err := gme.Open(smbFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	info, err := emu.TrackInfo(0)
	require.NoError(t, err)

	// NSF stores global metadata; libgme exposes it on every track.
	assert.Equal(t, "Super Mario Bros.", info.Game)
	assert.Equal(t, "Koji Kondo", info.Author)
	assert.Equal(t, "1985 Nintendo", info.Copyright)
	// play_length for plain NSF (no per-track duration) defaults to 150000ms (2.5 min).
	assert.Greater(t, info.PlayMs, 0)
}

func TestPlay_ProducesNonZeroSamples(t *testing.T) {
	emu, err := gme.Open(smbFixture(t), 44100)
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

func TestTrackEnded_AfterFade(t *testing.T) {
	emu, err := gme.Open(smbFixture(t), 44100)
	require.NoError(t, err)
	defer emu.Close()

	require.NoError(t, emu.StartTrack(0))
	emu.SetFade(0, 500) // start fading immediately, 500ms fade → ends after ~500ms

	buf := make([]int16, 4096) // ~46ms per iteration at 44100Hz stereo
	for i := 0; i < 50; i++ { // 50 × 46ms ≈ 2.3s — well past the 500ms fade
		if emu.TrackEnded() {
			return
		}
		require.NoError(t, emu.Play(buf))
	}
	t.Error("TrackEnded never returned true after fade")
}
