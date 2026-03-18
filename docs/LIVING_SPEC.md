# ChipFS — Living Specification

*This document describes the current architecture of ChipFS. It is updated as
the implementation evolves. For background on design decisions, see
[DESIGN.md](DESIGN.md). For accessible conceptual explanations, see
[CONCEPTS.md](CONCEPTS.md). For current task status, see [../TODO.md](../TODO.md).*

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

**Important:** These parsers are used for tasks that don't require an open
emulator — for example, scanning a directory to build the virtual filesystem
tree before any track is played. For per-track metadata during playback
(track count, title, duration), `gme.TrackInfo` is authoritative: it handles
NSFe quirks such as `plst` (playlist remapping) that the Go parsers do not.

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
`data` chunk (PCM bytes). The `id3 ` chunk is a standard RIFF extension that
taglib-based parsers (including Navidrome) read for Artist/Album/Title.

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

The CGO `#include` and linker flags are isolated to a `cgo.go` file (not yet
created) so that the rest of the package can be read without CGO context. The
libgme C API is stable and small (~20 functions); the wrapper is thin.

### `internal/vfs`

FUSE node implementations using `hanwen/go-fuse/v2`'s `NodeFS` API.

- **`Root`:** Top-level node. `OnAdd` scans the source directory at mount time;
  for each recognized chiptune file it adds a `RealFile` passthrough node and a
  virtual sibling `ChipDir`. go-fuse handles `Readdir`/`Lookup` automatically
  from the pre-populated inode tree.
- **`RealFile`:** Passthrough read of the original chiptune file on disk
  (`Getattr` + `Read` delegate to `os.Stat`/`os.ReadFile`).
- **`ChipDir`:** Virtual directory for one chiptune file. `OnAdd` opens the file
  with libgme to enumerate tracks and populates `TrackFile` children. go-fuse
  handles `Readdir`/`Lookup` from the pre-populated tree.
- **`TrackFile`:** Virtual WAV file for one track. `Getattr` reports
  `wav.EstimatedSize()`. `Read` implements **lazy emulation**: the WAV header
  bytes (RIFF + `fmt ` + `id3 ` chunks through the `data` chunk header) are
  pre-built at construction via `wav.HeaderBytes` and served without emulation.
  Only a read that reaches the PCM data region triggers a full render; the result
  is cached and all subsequent reads (including backward seeks) are served from
  the cache.

### `cmd/chipfs`

Entry point. Parses `-source` and `-mountpoint` flags, creates a `vfs.Root`,
mounts via `fs.Mount`, and blocks until SIGINT or SIGTERM (which triggers a clean
unmount).

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
Rendered: Super Mario Bros. — track 1/18 — 150000ms → /tmp/smb_track01.wav (26.5 MB)
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
       ├─ offset+len ≤ len(header)?
       │    YES → copy from pre-built header bytes, return   (no emulation)
       │
       └─ NO (read reaches PCM region):
            │  os.ReadFile("Mega_Man_2.nsf")
            │  gme.Open(nsfBytes, sampleRate=44100)
            │  emu.StartTrack(0)
            │  emu.SetFade(playMs, fadeMs)
            │  loop: emu.Play(chunk) → append to buffer
            │  trim samples to exact expected count
            │  wav.Encode(allSamples, opts) → wavBytes
            │  cache.Set("Mega_Man_2.nsf", 0, wavBytes)
            └─ copy bytes from wavBytes[offset:offset+size], return
```

The lazy emulation path is important for cold library scans: Navidrome reads
the first few KB of each file to extract metadata. Those reads land entirely
within the pre-built header and never trigger emulation, so scanning a library
of 200 chiptune files costs no render time.

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

Phases 1–5 are complete: format parsers, WAV muxer, LRU cache, libgme CGO
wrapper, and FUSE layer are fully implemented and tested. The filesystem can
be mounted with `go run ./cmd/chipfs -source <dir> -mountpoint <dir>`.

See [../TODO.md](../TODO.md) for the Phase 6 integration and hardening checklist.
