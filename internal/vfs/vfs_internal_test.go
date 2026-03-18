package vfs

// Internal tests (package vfs, not vfs_test) so we can reach unexported types.

import (
	"context"
	"os"
	"path/filepath"
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
	tracks := buildTrackList("../../testdata/fixtures/smb.nsf", 180_000, 8_000)
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
		go func() {
			defer wg.Done()
			dest := make([]byte, 4096)
			result, errno := tf.Read(context.Background(), nil, dest, pcmOffset)
			assert.Equal(t, syscall.Errno(0), errno) // require is not safe outside the test goroutine
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

// TestTrackFile_Read_RenderErrorReturnsEIO verifies that a render failure
// (libgme rejecting a corrupt source file) returns EIO to the FUSE client
// rather than crashing the process. The corrupt file has valid NSF magic but
// is too short for libgme to parse, so gme.Open returns an error.
func TestTrackFile_Read_RenderErrorReturnsEIO(t *testing.T) {
	// Build a minimal corrupt NSF: valid magic bytes + zeros, total 55 bytes.
	// This is well short of the 128-byte header libgme requires, so gme.Open
	// will return an error rather than an emulator handle.
	corrupt := make([]byte, 55)
	copy(corrupt, "NESM\x1a") // NSF magic; rest stays zero

	path := filepath.Join(t.TempDir(), "corrupt.nsf")
	require.NoError(t, os.WriteFile(path, corrupt, 0600))

	opts := wav.Options{SampleRate: 44100, Channels: 2}
	const totalMs = 1000
	header := wav.HeaderBytes(totalMs, opts)
	tf := &TrackFile{
		sourcePath:    path,
		trackIdx:      0,
		playMs:        900,
		fadeMs:        100,
		opts:          opts,
		header:        header,
		estimatedSize: wav.EstimatedSize(totalMs, opts),
		cache:         nil,
	}

	// Read at PCM offset to trigger renderTrack with the corrupt source.
	dest := make([]byte, 65536)
	result, errno := tf.Read(context.Background(), nil, dest, int64(len(header)))

	assert.Equal(t, syscall.EIO, errno, "render error must return EIO")
	assert.Nil(t, result, "render error must return nil result")
}

func TestBuildTrackList_SMB(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/smb.nsf", 180_000, 8_000)
	require.NotNil(t, tracks, "smb.nsf must be recognised as a chiptune file")

	assert.Equal(t, 18, len(tracks))

	// Plain NSF has no per-track titles → synthesised filenames.
	assert.Equal(t, "Track_01.wav", tracks[0].filename)
	assert.Equal(t, "Track_18.wav", tracks[17].filename)

	// Plain NSF has no per-track duration; clampMs returns our configured default.
	assert.Equal(t, 180_000, tracks[0].playMs)
	assert.Equal(t, 8_000, tracks[0].fadeMs)

	// Album should be populated from ti.Game.
	assert.Equal(t, "Super Mario Bros.", tracks[0].opts.Metadata.Album)
	assert.Equal(t, "Koji Kondo", tracks[0].opts.Metadata.Artist)
}

func TestBuildTrackList_DuckTales(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/ducktales.nsfe", 180_000, 8_000)
	require.NotNil(t, tracks, "ducktales.nsfe must be recognised as a chiptune file")

	// plst remapping: libgme reports 16 playlist entries.
	assert.Equal(t, 16, len(tracks))

	// First track should have a real title from tlbl (not synthesised).
	assert.NotEqual(t, "Track_01.wav", tracks[0].filename,
		"NSFe per-track titles should produce named filenames")
}

func TestBuildTrackList_UnknownExtension(t *testing.T) {
	assert.Nil(t, buildTrackList("/etc/hosts", 180_000, 8_000),
		"non-chiptune file must return nil")
}

func TestSanitizeFilename_ReplacesSlashAndColon(t *testing.T) {
	assert.Equal(t, "A_B_C", sanitizeFilename("A/B:C"))
	assert.Equal(t, "no change", sanitizeFilename("no change"))
}

func TestSanitizeFilename_StripsControlChars(t *testing.T) {
	// Control characters (0x01–0x1F, 0x7F) must be replaced with underscores.
	// They can appear in NSFe tlbl or SPC tag strings and would corrupt
	// filenames or terminal output if passed through.
	assert.Equal(t, "Title_Extra", sanitizeFilename("Title\x01Extra"))
	assert.Equal(t, "Title_Extra", sanitizeFilename("Title\x1fExtra"))
	assert.Equal(t, "Title_Extra", sanitizeFilename("Title\x7fExtra"))
	assert.Equal(t, "Title_Extra", sanitizeFilename("Title\tExtra")) // \t is 0x09
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
	result, errno := tf.Read(context.Background(), nil, dest, 0)
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
	result, errno := tf.Read(context.Background(), nil, dest, 0)
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
	tracks := buildTrackList("../../testdata/fixtures/smb.nsf", 180_000, 8_000)
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

// TestRealFileHandle_Read verifies that realFileHandle reads the correct bytes
// at arbitrary offsets from the underlying file.
func TestRealFileHandle_Read(t *testing.T) {
	f, err := os.CreateTemp("", "chipfs-realfile-*.bin")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	content := []byte("hello, world!")
	_, err = f.Write(content)
	require.NoError(t, err)
	f.Close()

	of, err := os.Open(f.Name())
	require.NoError(t, err)
	h := &realFileHandle{file: of}
	defer h.Release(nil)

	// Read at offset 0.
	dest := make([]byte, 5)
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno)
	b, st := result.Bytes(dest)
	require.Equal(t, 0, int(st))
	assert.Equal(t, []byte("hello"), b)

	// Read at non-zero offset.
	dest2 := make([]byte, 6)
	result2, errno2 := h.Read(nil, dest2, 7)
	require.Equal(t, syscall.Errno(0), errno2)
	b2, _ := result2.Bytes(dest2)
	assert.Equal(t, []byte("world!"), b2)
}

// TestRealFileHandle_Release_ClosesFile verifies that Release closes the
// underlying file descriptor so subsequent reads on it fail.
func TestRealFileHandle_Release_ClosesFile(t *testing.T) {
	f, err := os.CreateTemp("", "chipfs-realfile-*.bin")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.Write([]byte("data"))
	require.NoError(t, err)
	f.Close()

	of, err := os.Open(f.Name())
	require.NoError(t, err)
	h := &realFileHandle{file: of}

	errno := h.Release(nil)
	assert.Equal(t, syscall.Errno(0), errno)

	// After Release the fd is closed; ReadAt must fail.
	dest := make([]byte, 4)
	_, readErr := of.ReadAt(dest, 0)
	assert.Error(t, readErr, "file must be closed after Release")
}
