package vfs

// Internal tests (package vfs, not vfs_test) so we can reach unexported types.

import (
	"sync"
	"syscall"
	"testing"

	"github.com/gregpoulos/chipfs/internal/cache"
	"github.com/gregpoulos/chipfs/internal/wav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrackFile_ConcurrentReads verifies that concurrent reads of the same
// track all return consistent results. With -race this also catches data races
// in the cache and render path that singleflight is meant to protect.
func TestTrackFile_ConcurrentReads(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/smb.nsf")
	require.NotNil(t, tracks)
	t0 := tracks[0]
	totalMs := t0.totalMs()
	c := cache.New(256 * 1024 * 1024)
	tf := &TrackFile{
		sourcePath:    "../../testdata/fixtures/smb.nsf",
		trackIdx:      t0.trackIdx,
		playMs:        t0.playMs,
		fadeMs:        t0.fadeMs,
		opts:          t0.opts,
		header:        wav.HeaderBytes(totalMs, t0.opts),
		estimatedSize: wav.EstimatedSize(totalMs, t0.opts),
		cache:         c,
	}

	// Read from the PCM region so all goroutines trigger a render.
	pcmOffset := int64(len(tf.header))
	const goroutines = 8
	results := make([][]byte, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			dest := make([]byte, 4096)
			result, errno := tf.Read(nil, nil, dest, pcmOffset)
			require.Equal(t, syscall.Errno(0), errno)
			b, _ := result.Bytes(dest)
			results[i] = b
		}()
	}
	wg.Wait()

	// All goroutines must have received the same bytes.
	for i := 1; i < goroutines; i++ {
		assert.Equal(t, results[0], results[i], "goroutine %d got different result", i)
	}
}

// TestClampMs verifies the default, passthrough, and cap branches.
func TestClampMs(t *testing.T) {
	// zero/negative → default
	assert.Equal(t, 180_000, clampMs(0, 180_000, 20*60*1000))
	assert.Equal(t, 180_000, clampMs(-1, 180_000, 20*60*1000))
	// normal value → unchanged
	assert.Equal(t, 150_000, clampMs(150_000, 180_000, 20*60*1000))
	// exactly at max → allowed
	assert.Equal(t, 20*60*1000, clampMs(20*60*1000, 180_000, 20*60*1000))
	// over max → capped
	assert.Equal(t, 20*60*1000, clampMs(25*60*1000, 180_000, 20*60*1000))
	assert.Equal(t, 20*60*1000, clampMs(99*60*1000, 180_000, 20*60*1000))
}

// TestTrackFile_Read_PanicReturnsEIO verifies that a panic inside Read is
// recovered and returns EIO rather than crashing the process.
func TestTrackFile_Read_PanicReturnsEIO(t *testing.T) {
	opts := wav.Options{SampleRate: 44100, Channels: 2}
	header := wav.HeaderBytes(1000, opts)
	tf := &TrackFile{
		sourcePath:    "test.nsf",
		trackIdx:      3,
		header:        header,
		estimatedSize: -1, // make([]byte, end-off) panics when end clamps to -1
		cache:         nil,
	}

	dest := make([]byte, 65536)
	result, errno := tf.Read(nil, nil, dest, 0)

	assert.Equal(t, syscall.EIO, errno, "panic must return EIO")
	assert.Nil(t, result, "panic must return nil result")
}

func TestBuildTrackList_SMB(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/smb.nsf")
	require.NotNil(t, tracks, "smb.nsf must be recognised as a chiptune file")

	assert.Equal(t, 18, len(tracks))

	// Plain NSF has no per-track titles → synthesised filenames.
	assert.Equal(t, "Track_01.wav", tracks[0].filename)
	assert.Equal(t, "Track_18.wav", tracks[17].filename)

	// libgme's default play_length for plain NSF is 150 000 ms.
	assert.Equal(t, 150_000, tracks[0].playMs)
	assert.Equal(t, 8_000, tracks[0].fadeMs)

	// Album should be populated from ti.Game.
	assert.Equal(t, "Super Mario Bros.", tracks[0].opts.Metadata.Album)
	assert.Equal(t, "Koji Kondo", tracks[0].opts.Metadata.Artist)
}

func TestBuildTrackList_DuckTales(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/ducktales.nsfe")
	require.NotNil(t, tracks, "ducktales.nsfe must be recognised as a chiptune file")

	// plst remapping: libgme reports 16 playlist entries.
	assert.Equal(t, 16, len(tracks))

	// First track should have a real title from tlbl (not synthesised).
	assert.NotEqual(t, "Track_01.wav", tracks[0].filename,
		"NSFe per-track titles should produce named filenames")
}

func TestBuildTrackList_UnknownExtension(t *testing.T) {
	assert.Nil(t, buildTrackList("/etc/hosts"),
		"non-chiptune file must return nil")
}

func TestSanitizeFilename_ReplacesSlashAndColon(t *testing.T) {
	assert.Equal(t, "A_B_C", sanitizeFilename("A/B:C"))
	assert.Equal(t, "no change", sanitizeFilename("no change"))
}

func TestSanitizeFilename_RejectsDotDot(t *testing.T) {
	// A game title of ".." or "." must not produce a directory with special
	// path meaning; other names that merely contain dots are fine.
	assert.Equal(t, "_", sanitizeFilename(".."))
	assert.Equal(t, "_", sanitizeFilename("."))
	assert.Equal(t, "...And Justice for All", sanitizeFilename("...And Justice for All"))
	assert.Equal(t, "file.name", sanitizeFilename("file.name"))
}

func TestTrackFile_HeaderOnlyRead_NoEmulation(t *testing.T) {
	// Build a TrackFile with a known header but a source path that cannot be
	// opened by the emulator. If Read correctly serves from the pre-built
	// header without calling renderTrack, the test passes even though
	// rendering would fail.
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Test Track", Album: "Test Game"},
	}
	const totalMs = 10_000
	header := wav.HeaderBytes(totalMs, opts)
	tf := &TrackFile{
		sourcePath:    "/nonexistent/path/that/cannot/be/opened.nsf",
		trackIdx:      0,
		playMs:        totalMs - 8_000,
		fadeMs:        8_000,
		opts:          opts,
		header:        header,
		estimatedSize: wav.EstimatedSize(totalMs, opts),
		cache:         nil, // no cache — forces the header-only path
	}

	dest := make([]byte, 12) // first 12 bytes = RIFF chunk header
	result, errno := tf.Read(nil, nil, dest, 0)
	require.Equal(t, 0, int(errno))
	require.NotNil(t, result)

	got, st := result.Bytes(dest)
	require.Equal(t, 0, int(st))
	assert.Equal(t, header[:12], got, "first 12 bytes must match RIFF header")
}

// TestTrackFile_LargeBufferRead_HeaderPlusZeros verifies that a read starting
// in the header region with a buffer larger than the header returns the full
// requested size (header bytes + silence zeros), not a short read. This
// matches the behavior expected by streaming parsers like ffprobe which treat
// short reads on seekable files as truncation errors.
func TestTrackFile_LargeBufferRead_HeaderPlusZeros(t *testing.T) {
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Test Track", Album: "Test Game"},
	}
	const totalMs = 10_000
	header := wav.HeaderBytes(totalMs, opts)
	estimatedSize := wav.EstimatedSize(totalMs, opts)
	tf := &TrackFile{
		sourcePath:    "/nonexistent/path/that/cannot/be/opened.nsf",
		trackIdx:      0,
		playMs:        totalMs - 8_000,
		fadeMs:        8_000,
		opts:          opts,
		header:        header,
		estimatedSize: estimatedSize,
		cache:         nil,
	}

	// Large buffer, as a real FUSE client or ffprobe would use.
	dest := make([]byte, 65536)
	result, errno := tf.Read(nil, nil, dest, 0)
	require.Equal(t, 0, int(errno), "read must not error")
	require.NotNil(t, result)

	got, st := result.Bytes(dest)
	require.Equal(t, 0, int(st))

	// Must return full requested size (no short read).
	assert.Equal(t, len(dest), len(got), "must return full buffer, not a short read")
	// First bytes must be the real WAV header.
	assert.Equal(t, header, got[:len(header)], "header bytes must be correct")
	// Bytes beyond the header must be zeros (silence before render).
	for i := len(header); i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("byte %d beyond header is %d, want 0 (silence)", i, got[i])
		}
	}
}

func TestTrackFile_EstimatedSizeMatchesRenderOutput(t *testing.T) {
	// Full pipeline test using a real fixture: rendered WAV must have exactly
	// EstimatedSize bytes. This verifies the sample-trimming in renderTrack.
	tracks := buildTrackList("../../testdata/fixtures/smb.nsf")
	require.NotNil(t, tracks)

	// Use track 0; it's short enough with a quick fade for a unit test.
	t0 := tracks[0]
	tf := &TrackFile{
		sourcePath:    "../../testdata/fixtures/smb.nsf",
		trackIdx:      t0.trackIdx,
		playMs:        t0.playMs,
		fadeMs:        t0.fadeMs,
		opts:          t0.opts,
		header:        wav.HeaderBytes(t0.totalMs(), t0.opts),
		estimatedSize: wav.EstimatedSize(t0.totalMs(), t0.opts),
	}

	wavBytes, err := tf.renderTrack()
	require.NoError(t, err)

	assert.Equal(t, tf.estimatedSize, int64(len(wavBytes)),
		"renderTrack output must be exactly EstimatedSize bytes")
}
