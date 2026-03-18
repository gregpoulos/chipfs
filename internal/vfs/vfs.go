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
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gregpoulos/chipfs/internal/cache"
	gbsFmt "github.com/gregpoulos/chipfs/internal/formats/gbs"
	nsfFmt "github.com/gregpoulos/chipfs/internal/formats/nsf"
	spcFmt "github.com/gregpoulos/chipfs/internal/formats/spc"
	"github.com/gregpoulos/chipfs/internal/gme"
	"github.com/gregpoulos/chipfs/internal/wav"
	"github.com/hanwen/go-fuse/v2/fs"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sync/singleflight"
)

// defaultSampleRate is the PCM sample rate used for all renders. libgme and the
// WAV muxer both accept an arbitrary rate; this constant is the single place to
// change it. 44100 Hz matches CD quality and is universally supported.
const defaultSampleRate = 44100

const (
	// maxPlayMs and maxFadeMs are the upper bounds applied by clampMs and used
	// as the render-loop safety ceiling in renderTrack. Both must agree so a
	// clamped duration can never exceed the loop's stopping condition.
	maxPlayMs = 20 * 60 * 1000 // 20 minutes
	maxFadeMs = 60 * 1000      // 60 seconds
)

// Options configures a Root at mount time. Zero values are replaced with
// the built-in defaults listed below.
type Options struct {
	// DefaultPlayMs is the play duration used for tracks without embedded
	// duration metadata. Default: 180 000 ms (3 minutes).
	DefaultPlayMs int

	// DefaultFadeMs is the fade-out duration appended to DefaultPlayMs.
	// Default: 8 000 ms (8 seconds).
	DefaultFadeMs int

	// CacheBytes is the LRU cache capacity. Default: 256 MB.
	CacheBytes int64
}

func (o *Options) applyDefaults() {
	if o.DefaultPlayMs <= 0 {
		o.DefaultPlayMs = 180_000
	}
	if o.DefaultFadeMs <= 0 {
		o.DefaultFadeMs = 8_000
	}
	if o.CacheBytes <= 0 {
		o.CacheBytes = 256 * 1024 * 1024
	}
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

// Root is the top-level FUSE node. It scans the source directory on mount and
// adds child inodes for each file (real passthrough) and each recognized
// chiptune file (virtual ChipDir sibling).
type Root struct {
	fs.Inode
	sourceDir     string
	defaultPlayMs int
	defaultFadeMs int
	cache         *cache.Cache
	sf            *singleflight.Group
}

var _ fs.NodeOnAdder = (*Root)(nil)

// NewRoot creates a Root node backed by the given source directory.
// Zero-valued fields in opts are replaced with built-in defaults.
// Returns an error if sourceDir is empty or does not exist.
func NewRoot(sourceDir string, opts Options) (*Root, error) {
	if sourceDir == "" {
		return nil, fmt.Errorf("vfs: source directory must not be empty")
	}
	if _, err := os.Stat(sourceDir); err != nil {
		return nil, fmt.Errorf("vfs: source directory not accessible: %w", err)
	}
	opts.applyDefaults()
	return &Root{
		sourceDir:     sourceDir,
		defaultPlayMs: opts.DefaultPlayMs,
		defaultFadeMs: opts.DefaultFadeMs,
		cache:         cache.New(opts.CacheBytes),
		sf:            &singleflight.Group{},
	}, nil
}

// OnAdd is called by go-fuse when the root inode is initialized (at mount
// time). It scans the source directory and populates the virtual tree.
//
// The tree is a static snapshot: files added to the source directory after
// mounting are not visible until chipfs is restarted.
func (r *Root) OnAdd(ctx context.Context) {
	entries, err := os.ReadDir(r.sourceDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Only expose regular files. Symlinks are skipped deliberately: a
		// symlink pointing outside the source directory (e.g. to /etc/shadow)
		// would be followed transparently by RealFile, bypassing the source
		// directory boundary. Devices, pipes, and sockets are also skipped.
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		fullPath := filepath.Join(r.sourceDir, name)

		// Expose the real file as a passthrough node.
		rf := &RealFile{path: fullPath}
		rfInode := r.NewPersistentInode(ctx, rf, fs.StableAttr{Mode: syscall.S_IFREG})
		r.AddChild(name, rfInode, false)

		// For recognized chiptune files, also add a virtual ChipDir.
		tracks := buildTrackList(fullPath, r.defaultPlayMs, r.defaultFadeMs)
		if tracks == nil {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		dir := &ChipDir{
			sourcePath: fullPath,
			tracks:     tracks,
			cache:      r.cache,
			sf:         r.sf,
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

var _ fs.NodeOpener = (*RealFile)(nil)
var _ fs.NodeGetattrer = (*RealFile)(nil)
var _ fs.NodeReader = (*RealFile)(nil)

// Open opens the underlying file and returns a realFileHandle that holds the
// fd across all reads. go-fuse calls Release when the last open fd is closed.
func (f *RealFile) Open(_ context.Context, _ uint32) (fs.FileHandle, uint32, syscall.Errno) {
	file, err := os.Open(f.path)
	if err != nil {
		return nil, 0, syscall.ENOENT
	}
	return &realFileHandle{file: file}, gofuse.FOPEN_KEEP_CACHE, 0
}

func (f *RealFile) Getattr(_ context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	st, err := os.Stat(f.path)
	if err != nil {
		return syscall.ENOENT
	}
	out.Mode = syscall.S_IFREG | 0444
	out.Size = uint64(st.Size())
	return 0
}

// Read is a fallback for reads that arrive without an associated file handle
// (e.g. direct NodeReader calls in tests). Normal FUSE reads go through
// realFileHandle.Read once Open returns a handle.
func (f *RealFile) Read(_ context.Context, _ fs.FileHandle, dest []byte, off int64) (gofuse.ReadResult, syscall.Errno) {
	file, err := os.Open(f.path)
	if err != nil {
		return nil, syscall.EIO
	}
	defer file.Close()
	n, err := file.ReadAt(dest, off)
	if err != nil && err != io.EOF {
		return nil, syscall.EIO
	}
	return gofuse.ReadResultData(dest[:n]), 0
}

// ---------------------------------------------------------------------------
// realFileHandle
// ---------------------------------------------------------------------------

// realFileHandle holds an open *os.File across all FUSE read calls for a
// single open(2) / release(2) lifecycle. go-fuse dispatches reads to
// FileReader.Read and the final close to FileReleaser.Release.
type realFileHandle struct {
	file *os.File
}

var _ fs.FileReader = (*realFileHandle)(nil)
var _ fs.FileReleaser = (*realFileHandle)(nil)

func (h *realFileHandle) Read(_ context.Context, dest []byte, off int64) (gofuse.ReadResult, syscall.Errno) {
	n, err := h.file.ReadAt(dest, off)
	if err != nil && err != io.EOF {
		return nil, syscall.EIO
	}
	return gofuse.ReadResultData(dest[:n]), 0
}

func (h *realFileHandle) Release(_ context.Context) syscall.Errno {
	h.file.Close()
	return 0
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

// buildTrackList parses a chiptune file using the pure-Go format parsers and
// returns its track list. Returns nil if the file is not a recognised format
// or cannot be parsed. libgme is not called here; it is reserved for rendering
// in renderTrack, keeping mount-time scanning CGO-free.
//
// defaultPlayMs and defaultFadeMs are used as the clampMs fallback for tracks
// that have no embedded duration or fade metadata.
func buildTrackList(path string, defaultPlayMs, defaultFadeMs int) []trackEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".nsf", ".nsfe":
		h, err := nsfFmt.Parse(data)
		if err != nil {
			return nil
		}
		entries := make([]trackEntry, 0, h.TrackCount)
		for i := 0; i < h.TrackCount; i++ {
			var title string
			var playMs, fadeMs int
			if i < len(h.Tracks) {
				title = h.Tracks[i].Title
				playMs = h.Tracks[i].DurationMs
				fadeMs = h.Tracks[i].FadeMs
			}
			var filename string
			if title != "" {
				filename = fmt.Sprintf("%02d - %s.wav", i+1, sanitizeFilename(title))
			} else {
				filename = fmt.Sprintf("Track_%02d.wav", i+1)
				title = fmt.Sprintf("Track %d", i+1)
			}
			entries = append(entries, trackEntry{
				filename: filename,
				trackIdx: i,
				playMs:   clampMs(playMs, defaultPlayMs, maxPlayMs),
				fadeMs:   clampMs(fadeMs, defaultFadeMs, maxFadeMs),
				opts: wav.Options{
					SampleRate: defaultSampleRate,
					Channels:   2,
					Metadata: wav.Metadata{
						Title:  title,
						Artist: h.Artist,
						Album:  h.Title,
						Track:  i + 1,
					},
				},
			})
		}
		return entries

	case ".gbs":
		h, err := gbsFmt.Parse(data)
		if err != nil {
			return nil
		}
		entries := make([]trackEntry, 0, h.TrackCount)
		for i := 0; i < h.TrackCount; i++ {
			entries = append(entries, trackEntry{
				filename: fmt.Sprintf("Track_%02d.wav", i+1),
				trackIdx: i,
				playMs:   clampMs(0, defaultPlayMs, maxPlayMs),
				fadeMs:   clampMs(0, defaultFadeMs, maxFadeMs),
				opts: wav.Options{
					SampleRate: defaultSampleRate,
					Channels:   2,
					Metadata: wav.Metadata{
						Title:  fmt.Sprintf("Track %d", i+1),
						Artist: h.Author,
						Album:  h.Title,
						Track:  i + 1,
					},
				},
			})
		}
		return entries

	case ".spc":
		h, err := spcFmt.Parse(data)
		if err != nil {
			return nil
		}
		title := h.SongTitle
		if title == "" {
			title = "Track 1"
		}
		return []trackEntry{{
			filename: sanitizeFilename(title) + ".wav",
			trackIdx: 0,
			playMs:   clampMs(h.PlayDurationMs, defaultPlayMs, maxPlayMs),
			fadeMs:   clampMs(h.FadeDurationMs, defaultFadeMs, maxFadeMs),
			opts: wav.Options{
				SampleRate: defaultSampleRate,
				Channels:   2,
				Metadata: wav.Metadata{
					Title:  title,
					Artist: h.Artist,
					Album:  h.GameTitle,
					Track:  1,
				},
			},
		}}

	default:
		return nil
	}
}

// sanitizeFilename replaces characters that are invalid or problematic in
// POSIX filenames. Only '/' and '\x00' are strictly forbidden, but ':' is
// also replaced to avoid issues on macOS/Windows FUSE mounts. Control
// characters (0x01–0x1F, 0x7F) are replaced with '_' to prevent filename
// corruption and terminal escape injection from NSFe tlbl or SPC tag strings.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == ':':
			b.WriteByte('_')
		case r < 0x20 || r == 0x7f:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	result := strings.TrimSpace(b.String())
	if result == "." || result == ".." {
		return "_"
	}
	return result
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
	sf         *singleflight.Group
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
			sf:            d.sf,
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
	sf            *singleflight.Group
}

var _ fs.NodeOpener = (*TrackFile)(nil)
var _ fs.NodeGetattrer = (*TrackFile)(nil)
var _ fs.NodeReader = (*TrackFile)(nil)

// Open tells the FUSE kernel to use direct I/O for this virtual file, bypassing
// the kernel page cache. All reads come directly to our Read handler, which
// implements its own lazy-render + LRU cache logic.
func (f *TrackFile) Open(_ context.Context, _ uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, gofuse.FOPEN_DIRECT_IO, 0
}

func (f *TrackFile) Getattr(_ context.Context, _ fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | 0444
	out.Size = uint64(f.estimatedSize)
	return 0
}

// Read implements lazy emulation. Reads that fall entirely within the pre-built
// WAV header are served without touching the emulator. Only reads that reach
// the PCM data region trigger a full render.
func (f *TrackFile) Read(_ context.Context, _ fs.FileHandle, dest []byte, off int64) (result gofuse.ReadResult, errno syscall.Errno) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("vfs: TrackFile.Read panic (path=%s track=%d): %v", f.sourcePath, f.trackIdx, r)
			result, errno = nil, syscall.EIO
		}
	}()
	// Cache hit: full WAV already rendered.
	if f.cache != nil {
		if data, ok := f.cache.Get(f.sourcePath, f.trackIdx); ok {
			return gofuse.ReadResultData(sliceAt(data, dest, off)), 0
		}
	}

	// Cache miss: if the read starts within the pre-built header, serve the
	// header bytes and fill any requested bytes beyond the header with zeros.
	// Returning a full-sized response (no short read) prevents parsers like
	// ffprobe from treating the file as truncated. The zero-filled PCM region
	// is correct silence — the real PCM is served once a PCM-region read
	// triggers emulation and populates the cache.
	if off < int64(len(f.header)) {
		end := off + int64(len(dest))
		if end > f.estimatedSize {
			end = f.estimatedSize
		}
		result := make([]byte, end-off)
		copy(result, f.header[off:])
		// bytes beyond the header remain zero (silence before render)
		return gofuse.ReadResultData(result), 0
	}

	// Read touches the PCM region: render the full track. singleflight
	// ensures that concurrent misses for the same (sourcePath, trackIdx)
	// share a single render rather than duplicating the work.
	var (
		wavBytes []byte
		err      error
	)
	if f.sf != nil {
		key := fmt.Sprintf("%s\x00%d", f.sourcePath, f.trackIdx)
		v, sfErr, _ := f.sf.Do(key, func() (any, error) {
			return f.renderTrack()
		})
		if sfErr != nil {
			return nil, syscall.EIO
		}
		wavBytes = v.([]byte)
	} else {
		wavBytes, err = f.renderTrack()
		if err != nil {
			return nil, syscall.EIO
		}
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
	maxSamples := ((maxPlayMs + maxFadeMs) * sr / 1000) * ch
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

// clampMs returns ms if it is in (0, maxMs]; returns defaultMs if ms <= 0;
// returns maxMs if ms > maxMs.
func clampMs(ms, defaultMs, maxMs int) int {
	if ms <= 0 {
		return defaultMs
	}
	if ms > maxMs {
		return maxMs
	}
	return ms
}

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
