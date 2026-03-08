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

- **`Root`:** Top-level node. Scans the source directory; for each recognized
  chiptune file, exposes the real file (passthrough) and a virtual sibling `ChipDir`.
- **`ChipDir`:** Virtual directory for one chiptune file. `Readdir` synthesizes
  track filenames from metadata; `Lookup` returns a `TrackFile` node.
- **`TrackFile`:** Virtual WAV file for one track. `Getattr` reports
  `wav.EstimatedSize()`; `Read` serves bytes from the cache, triggering emulation
  if the cache is cold.

### `cmd/chipfs`

Entry point. Parses `-source` and `-mountpoint` flags and any mount options, then
calls `fuse.Mount`. Not yet implemented beyond flag parsing.

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
  │  check cache.Get("Mega_Man_2.nsf", trackIndex=0)
  ├─ cache HIT → copy bytes from cache buffer, return
  └─ cache MISS:
       │  gme.Open(nsfBytes, sampleRate=44100)
       │  emu.StartTrack(0)
       │  emu.SetFade(durationMs)
       │  loop: emu.Play(chunk) → append to buffer
       │  wav.Encode(allSamples, opts) → wavBytes
       │  cache.Set("Mega_Man_2.nsf", 0, wavBytes)
       └─ copy bytes from wavBytes[offset:offset+size], return
```

The emulation loop runs in a goroutine. Concurrent readers of the same track
share one emulation run via a `sync.Cond`-based wait on buffer growth (not yet
implemented; current stub blocks until full render completes).

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

All packages are present as stubs. Tests exist and currently fail. See
[../TODO.md](../TODO.md) for the phase-by-phase checklist.

The first implementation target is `internal/formats/nsf` — it has no dependencies
on other internal packages, requires no CGO, and its tests exercise a well-specified
binary format with a small fixed-size header.
