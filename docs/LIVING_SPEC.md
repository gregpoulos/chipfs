# ChipFS — Living Specification

*This document describes the current architecture of ChipFS. It is updated as
the implementation evolves. For background on design decisions, see
[DESIGN.md](DESIGN.md). For accessible conceptual explanations, see
[CONCEPTS.md](CONCEPTS.md). For current task status, see [TODO.md](TODO.md).*

---

## What ChipFS Does

ChipFS is a read-only FUSE filesystem. It mounts a directory containing chiptune
files (`.nsf`, `.gbs`, `.spc`) and presents each one as a virtual sibling
directory populated with virtual WAV files — one per track. The WAV files are
synthesized on demand by a real-time emulator and served to the OS as if they were
ordinary files on disk. No audio is ever written to the source directory.

The primary consumer is Navidrome, a self-hosted music server. Navidrome scans the
virtual directory, reads the ID3 tags embedded in each WAV, and presents the game
soundtracks as albums in its library.

---

## The Three Hard Problems

These problems are inherent to the architecture and their solutions are fixed design
decisions — not implementation details that change between sessions.

**1. File size before content:** FUSE's `getattr` must report a file size before
any audio is generated. ChipFS solves this by using WAV output, whose size is
mathematically exact: `(duration_ms / 1000) × sample_rate × channels × 2 + header`.
`wav.EstimatedSize()` must return the exact value that `wav.Encode()` produces.

**2. Tracks that never end:** NES and Game Boy music loops forever. ChipFS calls
`gme_set_fade(emu, start_ms)` to instruct libgme to fade out at a specified point.
The fade start is taken from NSFe `time` metadata when available; otherwise a
configurable default (180 seconds) is used.

**3. Seeking requires re-emulation:** Emulator state is not reversible. ChipFS
mitigates this by caching the entire rendered track in RAM after the first read.
Subsequent seeks — including backward seeks — are served from the cache with no
additional emulation cost.

---

## Architecture

ChipFS is organized into six internal packages with strict dependency ordering.
No package imports a package above it in this list.

### `internal/formats/{nsf,gbs,spc}`

Pure Go binary parsers. Each reads a file's header bytes using `encoding/binary`
and returns a `Header` struct. No I/O, no emulation, no CGO.

**Current status:** These parsers are the sole metadata source for the
mount-time directory scan. `buildTrackList` calls them directly — no CGO at
scan time. libgme is reserved for rendering only (`renderTrack`). NSFe `plst`
playlist remapping is handled by the Go parser: after parsing, `h.Tracks` is
already in playlist order and `h.TrackCount` equals the playlist length, so
`renderTrack`'s `emu.StartTrack(trackIdx)` (where `trackIdx` is the 0-indexed
playlist position) is consistent with what libgme reports.

- **NSF:** 128-byte header. Provides global title, artist, copyright, and track
  count. No per-track metadata. NSFe (extended NSF) adds chunk-based extensions
  including per-track titles (`tlbl`), durations (`time`), and fade lengths (`fade`).
- **GBS:** 0x70-byte header. Same structure as NSF: global metadata, track count,
  no per-track information.
- **SPC:** 33-byte magic + ID666 tag block at fixed offsets. One track per file.
  Provides song title, game title, artist, and an explicit play duration in seconds.

### `internal/wav`

Pure Go WAV muxer. Accepts `[]int16` PCM samples and `Options` (sample rate,
channels, metadata); returns a complete WAV `[]byte`.

The output format is: RIFF header → `fmt ` chunk → `id3 ` chunk (ID3v2 tag) →
`LIST INFO` chunk (INAM/IART/IPRD subchunks) → `data` chunk (PCM bytes). The
`id3 ` chunk is read by taglib-based parsers (including Navidrome); the `LIST INFO`
chunk provides the same metadata to older WAV parsers (Windows Media Player,
Winamp) that do not read `id3 `. Both coexist in every output file.

`EstimatedSize(durationMs, opts)` returns the exact byte count for a track of the
given duration. This value is reported to FUSE in `getattr` before emulation begins.

### `internal/cache`

Thread-safe LRU cache. Key: `(sourcePath string, trackIndex int)`. Value: `[]byte`
(complete rendered WAV). Implemented with `container/list` + `map` for O(1)
get/set/evict. Capacity is measured in bytes; eviction is LRU.

### `internal/gme`

CGO wrapper around `libgme` (Game Music Emu). Exposes `Open`, `TrackCount`,
`TrackInfo`, `StartTrack`, `SetFade`, `Play`, `TrackEnded`, `Close`. An `Emu`
wraps a `*C.Music_Emu` handle and is not safe for concurrent use.

The CGO preamble includes two version-gated C shims (`chipfs_set_fade`,
`chipfs_fade_length`) that bridge the API difference between libgme 0.6.3
(Debian bookworm) and 0.6.4 (Homebrew). See the `#if GME_VERSION >= 0x000604`
block in `gme.go`. Always use `//` comments inside CGO preambles — `/* */`
comments prematurely close the enclosing Go block comment.

### `internal/vfs`

FUSE node implementations using `hanwen/go-fuse/v2`'s `NodeFS` API.

- **`Root`:** Top-level node. `OnAdd` scans the source directory **once at mount
  time** and builds a static inode tree; new files added to the source directory
  after mounting are not visible until chipfs is restarted. Only regular files
  are exposed — symlinks, devices, and other special files are silently skipped
  to prevent a symlink from escaping the source directory boundary. go-fuse
  handles `Readdir`/`Lookup` automatically from the pre-populated tree.
- **`RealFile`:** Passthrough read of the original chiptune file on disk.
  `Open` opens an `*os.File` and returns a `realFileHandle` that holds it for
  the lifetime of the open/release pair; go-fuse dispatches reads to the handle's
  `FileReader.Read` and the final close to `FileReleaser.Release`. `Getattr`
  delegates to `os.Stat`.
- **`ChipDir`:** Virtual directory for one chiptune file. `OnAdd` iterates over
  the pre-scanned `[]trackEntry` (built by `buildTrackList` at mount time using
  the pure-Go parsers) and adds a `TrackFile` child for each entry. go-fuse
  handles `Readdir`/`Lookup` from the pre-populated tree.
- **`TrackFile`:** Virtual WAV file for one track. Implements `NodeOpener`
  (returns `FOPEN_DIRECT_IO` so all reads bypass the kernel page cache and reach
  our handler), `NodeGetattrer` (reports `wav.EstimatedSize()`), and `NodeReader`.
  `Read` implements **lazy emulation**: reads that start within the pre-built
  WAV header (RIFF + `fmt ` + `id3 ` + `LIST INFO` + `data` header) return those bytes plus
  zero-fill for any requested bytes beyond the header end — a full-sized response
  that avoids short reads while deferring emulation. Only a read whose offset
  reaches the PCM region triggers a full render; the result is cached and all
  subsequent reads (including backward seeks) are served from the LRU cache.

### `cmd/chipfs`

Entry point. Parses `-source`, `-mountpoint`, `-allow_other`, `-default_length`,
`-fade_length`, and `-cache_size_mb` flags, creates a `vfs.Root`, mounts via
`fs.Mount`, and blocks until SIGINT or SIGTERM (which triggers a clean unmount).

### `cmd/render`

Developer utility for manual integration testing. Renders a single track from a
chiptune file to a WAV file on disk, exercising the full pipeline
(format parser → libgme → WAV muxer) without requiring a FUSE mount.

```
go run ./cmd/render -file <path> -track <n> [-out <path>] [-duration <ms>] [-fade <ms>]
```

Flags:
- `-file` (required) — path to an NSF, NSFe, GBS, or SPC file
- `-track` — 0-indexed track number (default: 0)
- `-out` — output WAV path (default: `<stem>_track<N>.wav` in the current directory)
- `-duration` — override play duration in milliseconds (default: from file metadata, or 180000)
- `-fade` — fade-out length in milliseconds (default: 8000)

On success it prints a summary line:

```
Rendered: Pently demo — Track 1 — track 1/24 — 180000ms → /tmp/pently_track01.wav (30.2 MB)
```

This is a development tool only and is not part of the production filesystem binary.

---

## Data Flow

A complete read request from Navidrome through the stack:

```
Navidrome
  │  read("Mega_Man_2/01 - Flash Man.wav", offset=0, size=4096)
  ▼
Linux kernel FUSE module
  │  dispatches Read op to ChipFS process
  ▼
internal/vfs.TrackFile.Read(ctx, dest, offset)
  │
  ├─ cache HIT → copy bytes from cache buffer, return
  │
  └─ cache MISS:
       │
       ├─ offset < len(header)?
       │    YES → return header bytes + zero-fill to len(dest)  (no emulation)
       │
       └─ NO (read reaches PCM region):
            │  singleflight.Do("Mega_Man_2.nsf\x000") ─── coalesces concurrent misses
            │    os.ReadFile("Mega_Man_2.nsf")
            │    gme.Open(nsfBytes, sampleRate=44100)
            │    emu.StartTrack(0)
            │    emu.SetFade(playMs, fadeMs)
            │    loop: emu.Play(chunk) → append to buffer
            │    trim samples to exact expected count
            │    wav.Encode(allSamples, opts) → wavBytes
            │  cache.Set("Mega_Man_2.nsf", 0, wavBytes)
            └─ copy bytes from wavBytes[offset:offset+size], return
```

The lazy emulation path is important for cold library scans: Navidrome (and
tools like ffprobe) read the first few KB of each file to extract metadata.
Those reads start within the pre-built header and are served as header bytes
plus silence (zeros), never triggering emulation. Scanning a library of 200
chiptune files costs no render time.

---

## Supported Formats and Their Quirks

**NSF** — Track count is in the header; no per-track metadata. Virtual track
filenames are synthesized as `Track_{N:02d}.wav`. Virtual directory name comes
from the filename stem.

**NSFe** — Superset of NSF. Per-track titles from `tlbl` chunks become WAV
filenames. Per-track durations from `time` chunks replace the configured default.
Detection: magic bytes `"NSFE"` instead of `"NESM\x1A"`.

**GBS** — Structurally identical to NSF. No per-track metadata. Same synthesized
filename approach.

**SPC** — One track per file. Play duration is embedded in the ID666 tag (stored
as integer seconds; ChipFS multiplies by 1000 for milliseconds). The virtual
directory for `track.spc` contains exactly one file named from the `song_name`
ID666 field.

---

## Current Implementation Status

Phases 1–9 are complete. The filesystem mounts, serves virtual WAV files with
correct metadata and exact file sizes, and passes the Docker smoke test. All
hardening items (singleflight coalescing, LIST INFO RIFF chunk, RealFile
FileHandle, format parser split), test coverage (corrupt-fixture EIO test,
`-allow_other` smoke coverage, GitHub Actions CI), and mount options
(`-default_length`, `-fade_length`, `-cache_size_mb`) are done.

Deferred items (RSN support, FLAC output, N64/PSX formats, write support) remain
out of scope for v1. See [./TODO.md](./TODO.md).
