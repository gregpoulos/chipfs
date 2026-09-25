package vfs

// Internal tests (package vfs, not vfs_test) so we can reach unexported types.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregpoulos/chipfs/internal/wav"
	"github.com/hanwen/go-fuse/v2/fs"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrackFile_ConcurrentReads verifies that concurrent reads of the same
// track all return consistent results. With -race this also catches data races
// in the cache and render path that singleflight is meant to protect.
func TestTrackFile_ConcurrentReads(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/pently.nsf", 180_000, 8_000)
	require.NotNil(t, tracks)
	tf := newTrackFile("../../testdata/fixtures/pently.nsf", time.Time{}, tracks[0], newTrackStore(256*1024*1024))

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
	// default over max → capped too (e.g. -default_length 1500)
	assert.Equal(t, 20*60*1000, clampMs(0, 25*60*1000, 20*60*1000))
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
	tf := newTrackFile(path, time.Time{}, trackEntry{playMs: 900, fadeMs: 100, opts: opts}, newTrackStore(1<<20))

	// Read at PCM offset to trigger renderTrack with the corrupt source.
	dest := make([]byte, 65536)
	result, errno := tf.Read(context.Background(), nil, dest, int64(len(tf.header)))

	assert.Equal(t, syscall.EIO, errno, "render error must return EIO")
	assert.Nil(t, result, "render error must return nil result")
}

func TestBuildTrackList_Pently(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/pently.nsf", 180_000, 8_000)
	require.NotNil(t, tracks, "pently.nsf must be recognised as a chiptune file")

	assert.Equal(t, 24, len(tracks))

	// Plain NSF has no per-track titles → synthesised filenames.
	assert.Equal(t, "Track_01.wav", tracks[0].filename)
	assert.Equal(t, "Track_24.wav", tracks[23].filename)

	// Plain NSF has no per-track duration; clampMs returns our configured default.
	assert.Equal(t, 180_000, tracks[0].playMs)
	assert.Equal(t, 8_000, tracks[0].fadeMs)

	// Album and artist should be populated from NSF header.
	assert.Equal(t, "Pently demo", tracks[0].opts.Metadata.Album)
	assert.Equal(t, "DJ Tepples", tracks[0].opts.Metadata.Artist)
}

func TestBuildTrackList_PentlyNSFe(t *testing.T) {
	tracks := buildTrackList("../../testdata/fixtures/pently-demo.nsfe", 180_000, 8_000)
	require.NotNil(t, tracks, "pently-demo.nsfe must be recognised as a chiptune file")

	// plst remapping: 10 song tracks exposed, 15 sfx tracks hidden.
	assert.Equal(t, 10, len(tracks))

	// First track should have a real title from tlbl (not synthesised).
	assert.Equal(t, "01 - Argument?.wav", tracks[0].filename,
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

// TestTrackFile_ReadSpanningHeader_ReturnsHeaderOnly verifies that a large
// read starting in the header returns just the header (a short read) without
// rendering, so metadata probes with big buffers stay cheap. The source path
// cannot be opened, so any render attempt would fail with EIO.
func TestTrackFile_ReadSpanningHeader_ReturnsHeaderOnly(t *testing.T) {
	opts := wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata:   wav.Metadata{Title: "Test Track", Album: "Test Game"},
	}
	tf := newTrackFile("/nonexistent/path/that/cannot/be/opened.nsf", time.Time{},
		trackEntry{playMs: 2_000, fadeMs: 8_000, opts: opts}, newTrackStore(1<<20))

	dest := make([]byte, 131072)
	result, errno := tf.Read(context.Background(), nil, dest, 4)
	require.Equal(t, syscall.Errno(0), errno)
	got, _ := result.Bytes(dest)
	assert.Equal(t, tf.header[4:], got)
}

// TestTrackFile_ColdSequentialRead_MatchesRender reads a never-rendered track
// front to back in kernel-sized chunks and verifies the bytes served equal the
// rendered WAV. The PCM right after the header must be real audio, not
// placeholder silence served before the render ran.
func TestTrackFile_ColdSequentialRead_MatchesRender(t *testing.T) {
	const path = "../../testdata/fixtures/pently.nsf"
	t0 := buildTrackList(path, 10_000, 1_000)[0]
	want, err := newTrackFile(path, time.Time{}, t0, nil).renderTrack()
	require.NoError(t, err)

	tf := newTrackFile(path, time.Time{}, t0, newTrackStore(256*1024*1024))
	var got []byte
	dest := make([]byte, 131072)
	for {
		result, errno := tf.Read(context.Background(), nil, dest, int64(len(got)))
		require.Equal(t, syscall.Errno(0), errno)
		b, _ := result.Bytes(dest)
		if len(b) == 0 {
			break
		}
		got = append(got, b...)
	}
	require.Equal(t, len(want), len(got))
	assert.True(t, bytes.Equal(want, got), "cold sequential read must match the rendered WAV")
}

func TestTrackFile_EstimatedSizeMatchesRenderOutput(t *testing.T) {
	// Full pipeline test using a real fixture: rendered WAV must have exactly
	// EstimatedSize bytes. This verifies the sample-trimming in renderTrack.
	tracks := buildTrackList("../../testdata/fixtures/pently.nsf", 180_000, 8_000)
	require.NotNil(t, tracks)

	tf := newTrackFile("../../testdata/fixtures/pently.nsf", time.Time{}, tracks[0], nil)

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
	defer h.Release(context.Background())

	// Read at offset 0.
	dest := make([]byte, 5)
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno)
	b, st := result.Bytes(dest)
	require.Equal(t, 0, int(st))
	assert.Equal(t, []byte("hello"), b)

	// Read at non-zero offset.
	dest2 := make([]byte, 6)
	result2, errno2 := h.Read(context.Background(), dest2, 7)
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

	errno := h.Release(context.Background())
	assert.Equal(t, syscall.Errno(0), errno)

	// After Release the fd is closed; ReadAt must fail.
	dest := make([]byte, 4)
	_, readErr := of.ReadAt(dest, 0)
	assert.Error(t, readErr, "file must be closed after Release")
}

// copyFixture copies a testdata fixture to dst, creating parent directories.
func copyFixture(t *testing.T, name, dst string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../testdata/fixtures", name))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, data, 0o644))
}

// TestRoot_OnAdd_DescendsIntoSubdirectories verifies the source tree is
// mirrored: chiptune files in nested folders get a passthrough file and a
// virtual track directory beside them, and symlinked directories are skipped.
func TestRoot_OnAdd_DescendsIntoSubdirectories(t *testing.T) {
	src := t.TempDir()
	copyFixture(t, "pently.nsf", filepath.Join(src, "pently.nsf"))
	copyFixture(t, "ode-to-joy.spc", filepath.Join(src, "SNES", "Game", "ode.spc"))

	outside := t.TempDir()
	copyFixture(t, "pently.nsf", filepath.Join(outside, "secret.nsf"))
	require.NoError(t, os.Symlink(outside, filepath.Join(src, "linked")))

	root, err := NewRoot(src, Options{})
	require.NoError(t, err)
	fs.NewNodeFS(root, &fs.Options{}) // runs Root.OnAdd without a kernel mount

	assert.NotNil(t, root.GetChild("pently"), "top-level virtual dir must still exist")

	snes := root.GetChild("SNES")
	require.NotNil(t, snes, "subdirectory must be mirrored")
	game := snes.GetChild("Game")
	require.NotNil(t, game, "nested subdirectory must be mirrored")
	assert.NotNil(t, game.GetChild("ode.spc"), "passthrough file must appear in subdirectory")
	tracks := game.GetChild("ode")
	require.NotNil(t, tracks, "virtual track dir must appear beside nested file")
	assert.NotEmpty(t, tracks.Children(), "nested virtual dir must contain tracks")

	assert.Nil(t, root.GetChild("linked"), "symlinked directories must be skipped")
}

// TestFitSamples verifies rendered audio is forced to exactly the expected
// length: long output is trimmed and short output (libgme ending a track early
// on silence) is zero-padded, so the bytes served always match EstimatedSize.
func TestFitSamples(t *testing.T) {
	assert.Equal(t, []int16{1, 2}, fitSamples([]int16{1, 2, 3, 4}, 2), "long output is trimmed")
	assert.Equal(t, []int16{1, 2, 3}, fitSamples([]int16{1, 2, 3}, 3), "exact output is unchanged")
	assert.Equal(t, []int16{1, 2, 0, 0}, fitSamples([]int16{1, 2}, 4), "short output is zero-padded")
	assert.Equal(t, []int16{0, 0}, fitSamples(nil, 2), "empty output is all silence")
}

// TestGetattr_ReportsSourceTimestamps verifies every node reports the source's
// modification time rather than the zero epoch. Navidrome embeds the media
// file's mtime in stream tokens and rejects a zero value as a missing source
// timestamp (HTTP 410).
func TestGetattr_ReportsSourceTimestamps(t *testing.T) {
	src := t.TempDir()
	nsf := filepath.Join(src, "pently.nsf")
	copyFixture(t, "pently.nsf", nsf)
	copyFixture(t, "ode-to-joy.spc", filepath.Join(src, "SNES", "ode.spc"))

	fileTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	dirTime := time.Date(2021, 6, 7, 8, 9, 10, 0, time.UTC)
	require.NoError(t, os.Chtimes(nsf, fileTime, fileTime))
	require.NoError(t, os.Chtimes(filepath.Join(src, "SNES"), dirTime, dirTime))
	require.NoError(t, os.Chtimes(src, dirTime, dirTime))

	root, err := NewRoot(src, Options{})
	require.NoError(t, err)
	fs.NewNodeFS(root, &fs.Options{})

	attr := func(in *fs.Inode) gofuse.AttrOut {
		t.Helper()
		require.NotNil(t, in)
		var out gofuse.AttrOut
		errno := in.Operations().(fs.NodeGetattrer).Getattr(context.Background(), nil, &out)
		require.Equal(t, syscall.Errno(0), errno)
		return out
	}
	assertTimes := func(name string, out gofuse.AttrOut, want time.Time) {
		t.Helper()
		assert.Equal(t, uint64(want.Unix()), out.Mtime, name+" mtime")
		assert.Equal(t, uint64(want.Unix()), out.Ctime, name+" ctime")
		assert.Equal(t, uint64(want.Unix()), out.Atime, name+" atime")
	}

	chipDir := root.GetChild("pently")
	require.NotNil(t, chipDir)
	track := chipDir.GetChild("Track_01.wav")

	assertTimes("root", attr(&root.Inode), dirTime)
	assertTimes("real file", attr(root.GetChild("pently.nsf")), fileTime)
	assertTimes("chip dir", attr(chipDir), fileTime)
	assertTimes("track file", attr(track), fileTime)
	assertTimes("source dir", attr(root.GetChild("SNES")), dirTime)
}

// TestCleanTag verifies fixed-width padding is trimmed and rippers'
// "unknown" placeholders are treated as missing, so they can't split albums.
func TestCleanTag(t *testing.T) {
	assert.Equal(t, "SUPER MARIOWORLD", cleanTag("SUPER MARIOWORLD   "))
	assert.Equal(t, "Donkey Kong Country", cleanTag("Donkey Kong Country "))
	assert.Equal(t, "Koji Kondo", cleanTag("  Koji Kondo\t"))
	assert.Equal(t, "", cleanTag("<?>"))
	assert.Equal(t, "", cleanTag("?"))
	assert.Equal(t, "", cleanTag("  <?> "))
	assert.Equal(t, "", cleanTag(""))
	assert.Equal(t, "What?", cleanTag("What?"), "only a bare placeholder is missing")
}

func TestMostCommon(t *testing.T) {
	assert.Equal(t, "b", mostCommon([]string{"a", "b", "b", "c"}))
	assert.Equal(t, "a", mostCommon([]string{"b", "a"}), "ties break to the lexicographically smallest")
	assert.Equal(t, "x", mostCommon([]string{"", "", "x"}), "empty values are ignored")
	assert.Equal(t, "", mostCommon(nil))
	assert.Equal(t, "", mostCommon([]string{"", ""}))
}

// TestSPC_AlbumIsFolderName_AlbumArtistIsMostCommon verifies that SPC files
// group by their folder rather than by their inconsistent embedded tags: the
// album is the folder name, and the album artist is the folder's most common
// track artist, so per-track composers can't split the album.
func TestSPC_AlbumIsFolderName_AlbumArtistIsMostCommon(t *testing.T) {
	src := t.TempDir()
	folder := filepath.Join(src, "Chrono Trigger")
	for _, n := range []string{"a.spc", "b.spc", "c.spc"} {
		copyFixture(t, "ode-to-joy.spc", filepath.Join(folder, n))
	}
	// ode-to-joy.spc's tags are irrelevant: rewrite artist per file so two
	// share one composer and one differs.
	setArtist := func(name, artist string) {
		p := filepath.Join(folder, name)
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		for i := 0xB1; i < 0xD1; i++ {
			data[i] = 0
		}
		copy(data[0xB1:0xD1], artist)
		require.NoError(t, os.WriteFile(p, data, 0o644))
	}
	setArtist("a.spc", "Mitsuda")
	setArtist("b.spc", "Mitsuda")
	setArtist("c.spc", "Uematsu")

	root, err := NewRoot(src, Options{})
	require.NoError(t, err)
	fs.NewNodeFS(root, &fs.Options{})

	game := root.GetChild("Chrono Trigger")
	require.NotNil(t, game)
	for _, stem := range []string{"a", "b", "c"} {
		dir := game.GetChild(stem)
		require.NotNil(t, dir, stem)
		cd := dir.Operations().(*ChipDir)
		require.Len(t, cd.tracks, 1)
		md := cd.tracks[0].opts.Metadata
		assert.Equal(t, "Chrono Trigger", md.Album, stem+" album must be the folder name")
		assert.Equal(t, "Mitsuda", md.AlbumArtist, stem+" album artist must be the folder's most common artist")
	}
	assert.Equal(t, "Uematsu", game.GetChild("c").Operations().(*ChipDir).tracks[0].opts.Metadata.Artist,
		"track artist stays per-file")
}

// TestRoot_StemCollision verifies a chiptune whose virtual folder name is
// already taken (by a real entry or another chiptune with the same stem) gets
// "stem (ext)" instead of silently vanishing, and that an empty stem (a file
// named just ".nsf") doesn't crash the mount.
func TestRoot_StemCollision(t *testing.T) {
	src := t.TempDir()
	copyFixture(t, "seaside-village.gbs", filepath.Join(src, "game.gbs"))
	copyFixture(t, "pently.nsf", filepath.Join(src, "game.nsf"))
	copyFixture(t, "ode-to-joy.spc", filepath.Join(src, "song.spc"))
	require.NoError(t, os.Mkdir(filepath.Join(src, "song"), 0o755))
	copyFixture(t, "pently.nsf", filepath.Join(src, ".nsf"))

	root, err := NewRoot(src, Options{})
	require.NoError(t, err)
	fs.NewNodeFS(root, &fs.Options{})

	chipDir := func(name string) *ChipDir {
		t.Helper()
		in := root.GetChild(name)
		require.NotNil(t, in, name)
		cd, ok := in.Operations().(*ChipDir)
		require.True(t, ok, "%s must be a ChipDir", name)
		return cd
	}
	assert.Len(t, chipDir("game").tracks, 1, "first in sorted order (game.gbs) keeps the stem")
	assert.Len(t, chipDir("game (nsf)").tracks, 24)
	assert.IsType(t, &SourceDir{}, root.GetChild("song").Operations(), "real directory keeps its name")
	assert.Len(t, chipDir("song (spc)").tracks, 1)
	assert.Len(t, chipDir("(nsf)").tracks, 24)
}

// TestBuildTrackList_DoesNotReadUnrecognizedFiles verifies non-chiptune files
// are rejected by extension before being read, so a mount-time scan never
// loads videos or archives into memory. A FIFO makes any read observable:
// opening it blocks until a writer appears.
func TestBuildTrackList_DoesNotReadUnrecognizedFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "song.mp3")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	t.Cleanup(func() {
		// Unblock a reader stuck in open, if the test failed.
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
	})

	done := make(chan []trackEntry)
	go func() { done <- buildTrackList(fifo, 180_000, 8_000) }()
	select {
	case tracks := <-done:
		assert.Nil(t, tracks)
	case <-time.After(time.Second):
		t.Fatal("buildTrackList read a file it doesn't recognize")
	}
}

// TestBuildTrackList_Formats pins down per-format naming, titles, and duration
// sources: numbered files for multi-track formats, a bare title for SPC, and
// embedded durations winning over the configured defaults.
func TestBuildTrackList_Formats(t *testing.T) {
	const fixtures = "../../testdata/fixtures/"
	type want struct {
		filename, title, artist, album string
		playMs, fadeMs                 int
	}
	check := func(t *testing.T, got trackEntry, w want) {
		t.Helper()
		md := got.opts.Metadata
		assert.Equal(t, w, want{got.filename, md.Title, md.Artist, md.Album, got.playMs, got.fadeMs})
	}

	nsfe := buildTrackList(fixtures+"pently-demo.nsfe", 180_000, 8_000)
	check(t, nsfe[1], want{"02 - Isometry.wav", "Isometry", "DJ Tepples", "Pently demo", 192_000, 8_000})
	assert.Equal(t, 2, nsfe[1].opts.Metadata.Track)

	gbs := buildTrackList(fixtures+"seaside-village.gbs", 180_000, 8_000)
	require.Len(t, gbs, 1)
	check(t, gbs[0], want{"Track_01.wav", "Track 1", "Beatscribe", "Seaside Village", 180_000, 8_000})

	spc := buildTrackList(fixtures+"ode-to-joy.spc", 180_000, 8_000)
	require.Len(t, spc, 1)
	check(t, spc[0], want{"Ode To Joy (G Major).wav", "Ode To Joy (G Major)", "Ludwig van Beethoven", "fixtures", 14_000, 8_000})

	// An SPC without a song title is named after its (synthesized) title.
	data, err := os.ReadFile(fixtures + "ode-to-joy.spc")
	require.NoError(t, err)
	copy(data[0x2E:0x4E], make([]byte, 0x20))
	untitled := filepath.Join(t.TempDir(), "untitled.spc")
	require.NoError(t, os.WriteFile(untitled, data, 0o644))
	spc = buildTrackList(untitled, 180_000, 8_000)
	require.Len(t, spc, 1)
	assert.Equal(t, "Track 1.wav", spc[0].filename)
	assert.Equal(t, "Track 1", spc[0].opts.Metadata.Title)
}

// TestRenderTrack verifies the dev-tool entry point renders exactly what the
// mount would serve, with optional duration overrides.
func TestRenderTrack(t *testing.T) {
	const path = "../../testdata/fixtures/pently.nsf"

	got, err := RenderTrack(path, 1, 1_000, 500, Options{})
	require.NoError(t, err)
	assert.Equal(t, "Track_02.wav", got.Filename)
	assert.Equal(t, 24, got.TrackCount)
	assert.Equal(t, "Pently demo", got.Metadata.Album)
	assert.Equal(t, wav.EstimatedSize(1_500, wav.Options{SampleRate: 44100, Channels: 2, Metadata: got.Metadata}), int64(len(got.WAV)))

	// No overrides: the mount's defaults apply.
	got, err = RenderTrack(path, 0, 0, 0, Options{DefaultPlayMs: 2_000, DefaultFadeMs: 1_000})
	require.NoError(t, err)
	assert.Equal(t, 2_000, got.PlayMs)
	assert.Equal(t, 1_000, got.FadeMs)

	_, err = RenderTrack(path, 24, 0, 0, Options{})
	assert.Error(t, err, "track index out of range")
	_, err = RenderTrack("/etc/hosts", 0, 0, 0, Options{})
	assert.Error(t, err, "not a chiptune")
}
