package gme_test

import (
	"testing"

	"github.com/gregpoulos/chipfs/internal/gme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_RejectsZeroSampleRate(t *testing.T) {
	_, err := gme.Open([]byte("NESM\x1a"), 0)
	assert.ErrorIs(t, err, gme.ErrInvalidSampleRate)
}

func TestOpen_RejectsNegativeSampleRate(t *testing.T) {
	_, err := gme.Open([]byte("NESM\x1a"), -44100)
	assert.ErrorIs(t, err, gme.ErrInvalidSampleRate)
}

func TestOpen_RejectsInvalidData(t *testing.T) {
	// Data that is not a recognized chiptune format should return an error.
	_, err := gme.Open([]byte("this is not a music file"), 44100)
	assert.Error(t, err)
}

func TestEmu_TrackCount_BeforeOpen(t *testing.T) {
	// A freshly opened Emu should report a positive track count for a valid file.
	// This test is skipped until libgme CGO is implemented.
	t.Skip("requires libgme CGO implementation")

	// Once implemented, use a real fixture:
	// data, _ := os.ReadFile("../../testdata/fixtures/megaman2.nsf")
	// emu, err := gme.Open(data, 44100)
	// require.NoError(t, err)
	// defer emu.Close()
	// assert.Greater(t, emu.TrackCount(), 0)
}

func TestEmu_Play_ProducesSamples(t *testing.T) {
	// Verify that Play fills a buffer with non-zero samples for a real track.
	t.Skip("requires libgme CGO implementation and fixture file")

	_ = require.New(t) // suppress unused import warning while skipped
}
