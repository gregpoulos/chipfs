// Package vfs implements the FUSE filesystem nodes for ChipFS using hanwen/go-fuse.
//
// The package is intentionally thin: it translates FUSE kernel requests into
// calls to the format parsers, gme emulator, and track cache in other internal
// packages. All business logic lives in those packages; vfs only handles the
// mechanics of presenting virtual files and directories to the OS.
//
// # Virtual Directory Structure
//
// For each chiptune file foo.nsf in the source directory, ChipFS presents:
//
//	foo.nsf           (passthrough read of the real file)
//	foo/              (virtual directory, one entry per track)
//	  Track_01.wav    (virtual WAV file, rendered on demand)
//	  Track_02.wav
//	  ...
//
// # Lazy Emulation
//
// TrackFile.Read serves the WAV header (RIFF + fmt + id3 chunks) without
// starting the emulator. Only a read that reaches the PCM data region triggers
// a full render. This prevents a cold library scan from simultaneously rendering
// hundreds of tracks, which would spike RAM by hundreds of MB.
//
// # Node Types
//
//   - Root:      top-level node; lists source dir contents + virtual siblings
//   - RealFile:  passthrough read of a real file on disk
//   - ChipDir:   virtual directory for one chiptune file; populated in OnAdd
//   - TrackFile: virtual WAV file for one track; lazy emulation on Read
package vfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gregpoulos/chipfs/internal/cache"
	"github.com/gregpoulos/chipfs/internal/gme"
	"github.com/gregpoulos/chipfs/internal/wav"
	"github.com/hanwen/go-fuse/v2/fs"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
)

// defaultCacheBytes is the default LRU cache capacity (256 MB).
const defaultCacheBytes = 256 * 1024 * 1024

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

// Root is the top-level FUSE node. It scans the source directory on mount and
// adds child inodes for each file (real passthrough) and each recognized
// chiptune file (virtual ChipDir sibling).
type Root struct {
	fs.Inode
	sourceDir string
	cache     *cache.Cache
}

var _ fs.NodeOnAdder = (*Root)(nil)

// NewRoot creates a Root node backed by the given source directory.
// Returns an error if sourceDir is empty or does not exist.
func NewRoot(sourceDir string) (*Root, error) {
	if sourceDir == "" {
		return nil, fmt.Errorf("vfs: source directory must not be empty")
	}
	if _, err := os.Stat(sourceDir); err != nil {
		return nil, fmt.Errorf("vfs: source directory not accessible: %w", err)
	}
	return &Root{
		sourceDir: sourceDir,
		cache:     cache.New(defaultCacheBytes),
	}, nil
}

// OnAdd is called by go-fuse when the root inode is initialized (at mount
// time). It scans the source directory and populates the virtual tree.
func (r *Root) OnAdd(ctx context.Context) {
	entries, err := os.ReadDir(r.sourceDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		fullPath := filepath.Join(r.sourceDir, name)

		// Expose the real file as a passthrough node.
		rf := &RealFile{path: fullPath}
		rfInode := r.NewPersistentInode(ctx, rf, fs.StableAttr{Mode: syscall.S_IFREG})
		r.AddChild(name, rfInode, false)

		// For recognized chiptune files, also add a virtual ChipDir.
		tracks := buildTrackList(fullPath)
		if tracks == nil {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		dir := &ChipDir{
			sourcePath: fullPath,
			tracks:     tracks,
			cache:      r.cache,
		}
		dirInode := r.NewPersistentInode(ctx, dir, fs.StableAttr{Mode: syscall.S_IFDIR})
		r.AddChild(stem, dirInode, false)
	}
}

// ---------------------------------------------------------------------------
// RealFile
// ---------------------------------------------------------------------------

// RealFile is a passthrough read-only node that serves the bytes of a real
// file on disk. It is used to expose the original chiptune files alongside
// their virtual track directories.
type RealFile struct {
	fs.Inode
	path string
}

var _ fs.NodeGetattrer = (*RealFile)(nil)
var _ fs.NodeReader = (*RealFile)(nil)

func (f *RealFile) Getattr(_ context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	st, err := os.Stat(f.path)
	if err != nil {
		return syscall.ENOENT
	}
	out.Mode = syscall.S_IFREG | 0444
	out.Size = uint64(st.Size())
	return 0
}

func (f *RealFile) Read(_ context.Context, _ fs.FileHandle, dest []byte, off int64) (gofuse.ReadResult, syscall.Errno) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, syscall.EIO
	}
	return gofuse.ReadResultData(sliceAt(data, dest, off)), 0
}

// ---------------------------------------------------------------------------
// trackEntry — scan-time metadata for one track
// ---------------------------------------------------------------------------

type trackEntry struct {
	filename string      // e.g. "Track_01.wav" or "01 - Flash Man.wav"
	trackIdx int         // 0-indexed track number
	playMs   int         // fade start point (ms)
	fadeMs   int         // fade length (ms)
	opts     wav.Options // sample rate + channels + metadata
}

// totalMs returns the full rendered duration including the fade.
func (t trackEntry) totalMs() int { return t.playMs + t.fadeMs }

// buildTrackList opens a chiptune file with libgme and returns its track list.
// Returns nil if the file is not a recognized format or cannot be opened.
// Uses gme.TrackInfo as the authoritative source for per-track metadata,
// matching the behavior of TrackFile.renderTrack at playback time.
func buildTrackList(path string) []trackEntry {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".nsf", ".nsfe", ".gbs", ".spc":
	default:
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	emu, err := gme.Open(data, 44100)
	if err != nil {
		return nil
	}
	defer emu.Close()

	count := emu.TrackCount()
	entries := make([]trackEntry, 0, count)
	for i := 0; i < count; i++ {
		ti, err := emu.TrackInfo(i)
		if err != nil {
			continue
		}

		playMs := ti.PlayMs
		if playMs <= 0 {
			playMs = 180_000
		}
		fadeMs := ti.FadeMs
		if fadeMs <= 0 {
			fadeMs = 8_000
		}

		title := ti.Title
		if title == "" {
			title = fmt.Sprintf("Track %d", i+1)
		}

		var filename string
		if ti.Title != "" {
			filename = fmt.Sprintf("%02d - %s.wav", i+1, sanitizeFilename(ti.Title))
		} else {
			filename = fmt.Sprintf("Track_%02d.wav", i+1)
		}

		entries = append(entries, trackEntry{
			filename: filename,
			trackIdx: i,
			playMs:   playMs,
			fadeMs:   fadeMs,
			opts: wav.Options{
				SampleRate: 44100,
				Channels:   2,
				Metadata: wav.Metadata{
					Title:  title,
					Artist: ti.Author,
					Album:  ti.Game,
					Track:  i + 1,
				},
			},
		})
	}
	return entries
}

// sanitizeFilename replaces characters that are invalid or problematic in
// POSIX filenames. Only '/' and '\x00' are strictly forbidden, but ':' is
// also replaced to avoid issues on macOS/Windows FUSE mounts.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\x00', ':':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// ChipDir
// ---------------------------------------------------------------------------

// ChipDir is a virtual directory representing the tracks of one chiptune file.
// Its children (TrackFile nodes) are added in OnAdd from the pre-scanned track list.
type ChipDir struct {
	fs.Inode
	sourcePath string
	tracks     []trackEntry
	cache      *cache.Cache
}

var _ fs.NodeOnAdder = (*ChipDir)(nil)
var _ fs.NodeGetattrer = (*ChipDir)(nil)

func (d *ChipDir) Getattr(_ context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0555
	return 0
}

func (d *ChipDir) OnAdd(ctx context.Context) {
	for _, t := range d.tracks {
		totalMs := t.totalMs()
		tf := &TrackFile{
			sourcePath:    d.sourcePath,
			trackIdx:      t.trackIdx,
			playMs:        t.playMs,
			fadeMs:        t.fadeMs,
			opts:          t.opts,
			header:        wav.HeaderBytes(totalMs, t.opts),
			estimatedSize: wav.EstimatedSize(totalMs, t.opts),
			cache:         d.cache,
		}
		ch := d.NewPersistentInode(ctx, tf, fs.StableAttr{Mode: syscall.S_IFREG})
		d.AddChild(t.filename, ch, false)
	}
}

// ---------------------------------------------------------------------------
// TrackFile
// ---------------------------------------------------------------------------

// TrackFile is a virtual WAV file for one track of a chiptune file.
//
// Getattr reports the exact file size (wav.EstimatedSize) before any audio is
// rendered. Read serves the WAV header (RIFF + fmt + id3 chunks) without
// emulation. Only when a read reaches the PCM data region does renderTrack
// run the emulator, cache the result, and serve from cache thereafter.
type TrackFile struct {
	fs.Inode
	sourcePath    string
	trackIdx      int
	playMs        int
	fadeMs        int
	opts          wav.Options
	header        []byte // WAV bytes before PCM data; pre-built at construction
	estimatedSize int64
	cache         *cache.Cache
}

var _ fs.NodeGetattrer = (*TrackFile)(nil)
var _ fs.NodeReader = (*TrackFile)(nil)

func (f *TrackFile) Getattr(_ context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | 0444
	out.Size = uint64(f.estimatedSize)
	return 0
}

// Read implements lazy emulation. Reads that fall entirely within the pre-built
// WAV header are served without touching the emulator. Only reads that reach
// the PCM data region trigger a full render.
func (f *TrackFile) Read(_ context.Context, _ fs.FileHandle, dest []byte, off int64) (gofuse.ReadResult, syscall.Errno) {
	// Cache hit: full WAV already rendered.
	if f.cache != nil {
		if data, ok := f.cache.Get(f.sourcePath, f.trackIdx); ok {
			return gofuse.ReadResultData(sliceAt(data, dest, off)), 0
		}
	}

	// Cache miss: if the read falls entirely within the pre-built header, serve
	// it without emulation.
	if off+int64(len(dest)) <= int64(len(f.header)) {
		return gofuse.ReadResultData(sliceAt(f.header, dest, off)), 0
	}

	// Read touches the PCM region: render the full track.
	wavBytes, err := f.renderTrack()
	if err != nil {
		return nil, syscall.EIO
	}
	if f.cache != nil {
		f.cache.Set(f.sourcePath, f.trackIdx, wavBytes)
	}
	return gofuse.ReadResultData(sliceAt(wavBytes, dest, off)), 0
}

// renderTrack opens the source file, runs the emulator for the configured
// duration, muxes the samples into a WAV, and returns the complete byte slice.
// The sample slice is trimmed to exactly playMs+fadeMs worth of audio so that
// the rendered file size matches EstimatedSize precisely.
func (f *TrackFile) renderTrack() ([]byte, error) {
	data, err := os.ReadFile(f.sourcePath)
	if err != nil {
		return nil, fmt.Errorf("reading source: %w", err)
	}
	emu, err := gme.Open(data, f.opts.SampleRate)
	if err != nil {
		return nil, fmt.Errorf("opening with libgme: %w", err)
	}
	defer emu.Close()

	if err := emu.StartTrack(f.trackIdx); err != nil {
		return nil, fmt.Errorf("starting track: %w", err)
	}
	emu.SetFade(f.playMs, f.fadeMs)

	const chunkLen = 4096
	sr, ch := f.opts.SampleRate, f.opts.Channels
	maxSamples := 15 * 60 * sr * ch
	capacity := ((f.playMs + f.fadeMs) * sr / 1000) * ch
	allSamples := make([]int16, 0, capacity)
	chunk := make([]int16, chunkLen)

	for !emu.TrackEnded() && len(allSamples) < maxSamples {
		if err := emu.Play(chunk); err != nil {
			return nil, fmt.Errorf("rendering: %w", err)
		}
		allSamples = append(allSamples, chunk...)
	}

	// Trim to the expected sample count so the output size matches EstimatedSize
	// exactly. The render loop reads in 4096-sample chunks and may overshoot by
	// up to one chunk of silence past the fade end.
	expectedSamples := ((f.playMs + f.fadeMs) * sr / 1000) * ch
	if len(allSamples) > expectedSamples {
		allSamples = allSamples[:expectedSamples]
	}

	return wav.Encode(allSamples, f.opts)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sliceAt returns the portion of src that satisfies a read of len(dest) bytes
// starting at off, clamped to src's bounds.
func sliceAt(src, dest []byte, off int64) []byte {
	if off >= int64(len(src)) {
		return nil
	}
	end := off + int64(len(dest))
	if end > int64(len(src)) {
		end = int64(len(src))
	}
	return src[off:end]
}
