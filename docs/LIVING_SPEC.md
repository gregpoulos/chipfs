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
- **SPC:** 33-byte magic (`SNES-SPC700 Sound File Data v0.` plus any two-character version, e.g. `10`, `30`; matches libgme) + ID666 tag block at fixed offsets. One track per file.
  Provides song title, game title, artist, and an explicit play duration in seconds.

### `internal/wav`

Pure Go WAV muxer. Accepts `[]int16` PCM samples and `Options` (sample rate,
channels, metadata); returns a complete WAV `[]byte`.

The output format is: RIFF header → `fmt ` chunk → `id3 ` chunk (ID3v2 tag) →
`LIST INFO` chunk (INAM/IART/IPRD subchunks) → `data` chunk (PCM bytes). The
`id3 ` chunk is read by taglib-based parsers (including Navidrome); the `LIST INFO`
chunk provides the same metadata to older WAV parsers (Windows Media Player,
Winamp) that do not read `id3 `. Both coexist in every output file.

The ID3 tag also carries `TPE2` (album artist) when `Metadata.AlbumArtist` is set;
it has no `LIST INFO` equivalent.

`EstimatedSize(durationMs, opts)` returns the exact byte count for a track of the
given duration; it is reported to FUSE in `getattr` before emulation begins.
`Encode`, `HeaderBytes` (the pre-PCM prefix served before rendering), and
`EstimatedSize` all build from one internal header function and `SampleCount`,
so they cannot disagree. `vfs` sizes its render with `SampleCount` too.

### `internal/cache`

Thread-safe LRU cache. Key: `(sourcePath string, trackIndex int)`. Value: `[]byte`
(complete rendered WAV). Implemented with `container/list` + `map` for O(1)
get/set/evict. Capacity is measured in bytes; eviction is LRU.

### `internal/gme`

CGO wrapper around `libgme` (Game Music Emu). Exposes `Open`, `TrackCount`,
`StartTrack`, `SetFade`, `Play`, `TrackEnded`, `Close`. An `Emu`
wraps a `*C.Music_Emu` handle and is not safe for concurrent use.

A version-gated C shim in the CGO preamble (`chipfs_set_fade`) bridges the
API difference between libgme 0.6.3 (Debian bookworm) and 0.6.4 (Homebrew).
Metadata comes from the pure-Go parsers, not libgme.

### `internal/vfs`

FUSE node implementations using `hanwen/go-fuse/v2`'s `NodeFS` API.

- **`Root`:** Top-level node. `OnAdd` scans the source directory tree
  **recursively, once at mount time** and builds a static inode tree that
  mirrors the source layout; new files added to the source directory after
  mounting are not visible until chipfs is restarted. Only regular files and
  real directories are exposed — symlinks (including symlinked directories),
  devices, and other special files are silently skipped to prevent a symlink
  from escaping the source directory boundary or forming a cycle. go-fuse
  handles `Readdir`/`Lookup` automatically from the pre-populated tree.
  A chiptune's virtual folder is named after its file stem; if a real entry or
  an earlier (sorted) chiptune already holds that name, it becomes `stem (ext)`
  (e.g. `game (nsf)`) with a logged warning, rather than being dropped.
- **`SourceDir`:** Mirror of a real subdirectory. Populated by `Root` during
  the same scan; recognized chiptune files inside it get a passthrough file and
  a virtual `ChipDir` sibling exactly as at the top level.
- **Tags:** All tag strings pass through `cleanTag` (whitespace trimmed; a bare `?`
  or `<?>` treated as missing) so fixed-width padding and rippers' placeholders
  can't split one album into several. For SPC files the album is always the
  **parent folder's name**, not the embedded game tag: an SPC album is a folder of
  single-track files, and the embedded tag varies within one game's folder. Each
  SPC track's album artist (`TPE2`) is the folder's most common track artist
  (ties break alphabetically), so per-track composers don't split the album in
  taglib-based servers like Navidrome; the track artist stays per-file. NSF and
  GBS tracks get no album artist. Virtual nodes report their source's mtime
  (`setTimes`), since Navidrome rejects a zero timestamp when streaming.
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
  `Read` implements **lazy emulation**: a read that starts within the pre-built
  WAV header (RIFF + `fmt ` + `id3 ` + `LIST INFO` + `data` header) returns only
  header bytes, even if it asked for more. That short read is safe under
  `FOPEN_DIRECT_IO`: the client simply reads again at the PCM offset. Only a read
  whose offset reaches the PCM region triggers a full render; the result is cached
  and all subsequent reads (including backward seeks) are served from the LRU
  cache. Never pad a header read with placeholder PCM: sequential readers would
  hear it as silence at the start of the track.

### `cmd/chipfs`

Entry point. Parses `-source`, `-mountpoint`, `-allow_other`, `-default_length`,
`-fade_length`, and `-cache_size_mb` flags, creates a `vfs.Root`, mounts via
`fs.Mount`, and blocks until SIGINT or SIGTERM (which triggers a clean unmount).

Mounts set go-fuse's `DirectMount`: root calls `mount(2)` itself and falls back
to `fusermount3` on failure. This exists because Ubuntu's `fusermount3`
AppArmor profile revokes inherited file descriptors inside LXC, which breaks
the helper's socket handshake with go-fuse.

### `cmd/render`

Developer utility: renders one track to a WAV file on disk via
`vfs.RenderTrack`, which uses the mount's own track list and render path, so
the output matches what the mount serves (except SPC album artist, which is
computed per folder at mount time). `-duration`/`-fade` override the track's
durations; see `go run ./cmd/render -h`.

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
       │    YES → return header bytes only (short read, no emulation)
       │
       └─ NO (read reaches PCM region):
            │  trackStore.render: singleflight.Do("Mega_Man_2.nsf\x000") ─── coalesces concurrent misses
            │    cache hit (a render just finished)? → use it
            │    os.ReadFile("Mega_Man_2.nsf")
            │    gme.Open(nsfBytes, sampleRate=44100)
            │    emu.StartTrack(0)
            │    emu.SetFade(playMs, fadeMs)
            │    loop: emu.Play(chunk) → append to buffer
            │    trim samples to exact expected count
            │    wav.Encode(allSamples, opts) → wavBytes
            │    cache.Set("Mega_Man_2.nsf", 0, wavBytes)
            └─ copy bytes from wavBytes[offset:offset+size], return
```

The lazy emulation path is important for cold library scans: Navidrome (and
tools like ffprobe) read the first few KB of each file to extract metadata.
Those reads start within the pre-built header and are served from it,
never triggering emulation. Scanning a library of 200
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
ID666 field. ID666 has text and binary layouts with no reliable marker; the
guess can mistake a text tag with blank durations for binary, so the binary path
discards implausible durations and reads the artist from the text offset when
0xB0 is padding.

---

## Current Implementation Status

Phases 1–9 are complete. The filesystem mounts, serves virtual WAV files with
correct metadata and exact file sizes, and passes the Docker smoke test. All
hardening items (singleflight coalescing, LIST INFO RIFF chunk, RealFile
FileHandle, format parser split), test coverage (corrupt-fixture EIO test,
`-allow_other` smoke coverage, GitHub Actions CI), and mount options
(`-default_length`, `-fade_length`, `-cache_size_mb`) are done.

Deferred items (RSN support, FLAC output, N64/PSX formats, write support) remain
out of scope for v1. See [TODO.md](TODO.md).
