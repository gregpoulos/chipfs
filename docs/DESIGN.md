# ChipFS — Design Document

*This is a historical document capturing design decisions made at project inception.
It is frozen. For current implementation state, see [LIVING_SPEC.md](LIVING_SPEC.md)
and [../TODO.md](../TODO.md).*

---

## Problem Statement

Video game music from the 8-bit and 16-bit eras is stored as executable code, not
audio recordings. An `.nsf` file contains the NES music engine and audio data for
an entire game's soundtrack in a few kilobytes. Media servers like Navidrome cannot
play or scan these files. The goal is a FUSE layer that presents them as playable,
tagged WAV files without converting or duplicating the source collection on disk.

---

## The Three Hard Problems

### 1. File Size Must Be Reported Before Content Exists

FUSE's `getattr` (and the Go equivalent `Attr()`) must return a file size
synchronously, before any audio has been generated. The chosen solution:

**Use WAV output.** WAV (PCM) file size is mathematically exact:

```
size = (duration_ms / 1000) × sample_rate × channels × 2  +  header_overhead
```

This eliminates estimation entirely. The `wav.EstimatedSize()` function must
return the exact byte count that `wav.Encode()` produces for the same duration —
the test `TestEstimatedSize_MatchesActualEncodeOutput` enforces this invariant.

MP3 and FLAC were considered and rejected for v1: CBR MP3 can be estimated but
adds encoder complexity; FLAC estimation introduces ±10–15% error.

**Duration source:** NSFe `time` chunks and SPC ID666 `play_time` provide exact
per-track durations. Plain NSF and GBS have no duration metadata; a configurable
hard limit (default: 180 seconds) with a fade-out is used, matching the convention
of every major chiptune player.

### 2. Tracks Loop Forever

NES and Game Boy music code is designed to play indefinitely. The emulator must
be told when to stop.

**Decision:** Use `libgme`'s built-in `gme_set_fade(emu, start_ms)`. When
`start_ms` is reached, the library applies a linear amplitude fade over the next
few seconds and sets `gme_track_ended()` to true. ChipFS passes either the
NSFe-provided duration or the configured default to `gme_set_fade`.

Silence detection (scanning for quiet passages to find natural loop boundaries)
is deferred to a future phase.

### 3. Seeking Requires Re-Emulation from Start

Emulator state is not reversible: producing sample N requires computing all
samples 0..N-1. A backward seek cannot be satisfied from the current position.

**Decision:** Cache the entire rendered track in RAM after the first read. The
emulator runs at ~100–900× real time depending on the system; a 3-minute track
renders in well under a second. Once cached, any seek — forward or backward — is
served from the buffer at zero additional cost.

The `cache.Cache` LRU store is the mechanism. Capacity is configurable
(default: 256MB, holding ~8 fully-rendered 3-minute stereo tracks).

---

## Language and Library Choices

### Go with `hanwen/go-fuse/v2`

Go was chosen over Rust for this project:

- `hanwen/go-fuse` v2 is actively maintained (v2.9, 2025) and production-proven
  (used by rclone and other widely-deployed tools).
- Go's `encoding/binary` makes binary header parsing straightforward.
- Goroutines simplify the producer-consumer pattern between the emulator thread
  and FUSE read handlers.
- CGO bindings for `libgme` are ~1 day of work given the small C API surface.

Rust with `fuser` was a viable alternative (also actively maintained, February
2026 release). The existing `game-music-emu` Rust crate would have reduced binding
effort. Rust was not chosen because Go offers faster iteration for a solo project
and the Rust FUSE ecosystem is younger.

### `libgme` (Game Music Emu)

`libgme` is the reference-quality emulator library for classic console audio. It
exposes a clean C API (`gme.h`, ~20 functions), supports NSF, NSFe, GBS, SPC, GYM,
VGM, and more, and handles fade-out natively via `gme_set_fade`. No maintained
pure-Go or pure-Rust port exists.

The LGPL 2.1 license is compatible with MIT-licensed ChipFS under dynamic linking,
which is CGO's default behavior.

---

## Output Format

**WAV (PCM), stereo, 44100 Hz, int16.** No runtime encoder dependency; exact file
sizes; universally supported by Navidrome and every audio player.

FLAC output is a planned future option for users who want smaller cache footprints
and better compression. It requires a pure-Go FLAC encoder and introduces size
estimation complexity.

---

## Metadata Mapping

| Virtual WAV Field | NSF | NSFe | GBS | SPC |
|---|---|---|---|---|
| Title | `"{Game} - Track {N:02d}"` | `tlbl[N]` (per-track) | `"{Game} - Track {N:02d}"` | `song_name` |
| Artist | `artist` header field | `auth.artist` | `author` header field | `artist_name` |
| Album | `title` header field | `auth.game` | `title` header field | `game_title` |
| Track | 1-based index | 1-based index | 1-based index | 1 |
| Duration | Configured default | `time[N]` ms | Configured default | `play_time` × 1000 |

Tags are written as an ID3v2 block embedded in a RIFF `id3 ` chunk, the standard
location for ID3 metadata in WAV files. Taglib (which Navidrome uses internally)
reads this location.

---

## Virtual Directory Structure

```
/mnt/chipfs/
└── NES/
    ├── Mega_Man_2.nsf              ← passthrough (real file)
    └── Mega_Man_2/                 ← virtual directory
        ├── 01 - Flash Man.wav      ← virtual WAV (rendered on demand)
        ├── 02 - Air Man.wav
        └── ...
```

Virtual directory names are derived from NSFe `auth.game` if available, otherwise
from the source filename stem. Virtual track filenames use NSFe `tlbl` titles if
available, otherwise `Track_{N:02d}.wav`.

SPC files (one track per file) produce a single-entry virtual directory. RSN
archives (RAR collections of SPC files) are treated as directories of SPCs
(Phase 2, requires libarchive).

---

## Component Responsibilities

**`internal/formats/{nsf,gbs,spc}`** — Pure Go binary parsers. Read only the
file header; produce a `Header` struct with metadata and track count. No I/O,
no emulation.

**`internal/wav`** — Pure Go WAV muxer. Accepts `[]int16` PCM samples and
`Metadata`; produces a complete WAV `[]byte` with embedded ID3v2 tag. Also
exposes `EstimatedSize` for use by FUSE `getattr`.

**`internal/cache`** — Thread-safe LRU cache. Keyed by `(sourcePath, trackIndex)`.
Stores fully-rendered WAV bytes. Uses `container/list` for O(1) LRU operations.

**`internal/gme`** — CGO wrapper around `libgme`. Handles resource lifecycle
(`Open` → `StartTrack` → `SetFade` → `Play` loop → `Close`), converting C types
to Go types at the boundary.

**`internal/vfs`** — FUSE node implementations using `hanwen/go-fuse/v2`.
`Root` scans the source directory; `ChipDir` presents virtual tracks via `Readdir`
and `Lookup`; `TrackFile` serves WAV bytes from the cache via `Read`.

**`cmd/chipfs`** — Entry point. Parses flags, validates inputs, calls `fuse.Mount`.

---

## Prior Art

- **mp3fs / ffmpegfs** — Transcoding FUSE filesystems. Confirmed the linear-buffer
  seeking model and CBR size estimation approach. ChipFS adopts the same architecture
  but uses WAV output for exact sizes.
- **bazil/zipfs** — Structural template for the 1-to-many mapping pattern
  (one archive → many virtual files). ChipFS's `ChipDir` directly parallels `zipfs`'s
  `Dir` node.
- **fuse-archive (Google)** — Validated the three-tier caching model
  (full cache / lazy cache / no cache). ChipFS implements full in-memory cache
  with optional on-disk cache as a future option.
