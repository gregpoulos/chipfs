# ChipFS — Implementation TODO

This file tracks the current state of implementation. Update it as phases complete.

## Phase 1: Format Parsers ✓

The format parsers are the natural starting point: they are pure Go (no CGO, no
FUSE), their correctness is fully verifiable with unit tests against synthesized
binary fixtures, and all subsequent packages depend on the metadata they produce.

- [x] `internal/formats/nsf` — NSF header parser (magic, track count, global metadata)
- [x] `internal/formats/nsf` — NSFe extension chunk parser (tlbl, time, fade, auth)
- [x] `internal/formats/gbs` — GBS header parser
- [x] `internal/formats/spc` — SPC ID666 tag parser (text and binary format)

## Phase 2: WAV Muxer (current)

- [ ] `internal/wav` — RIFF WAV header construction
- [ ] `internal/wav` — ID3v2 tag building (TIT2, TPE1, TALB, TRCK, TDRC frames)
- [ ] `internal/wav` — Embed ID3v2 as RIFF `id3 ` chunk
- [ ] `internal/wav` — `EstimatedSize` must exactly match `Encode` output

## Phase 3: Track Cache

- [ ] `internal/cache` — LRU eviction using `container/list` + `map`
- [ ] `internal/cache` — Thread-safe Get/Set
- [ ] `internal/cache` — Byte-accurate capacity tracking

## Phase 4: libgme CGO Wrapper

- [ ] `internal/gme` — CGO import block with correct Homebrew/apt flags
- [ ] `internal/gme` — `Open` via `gme_open_data`
- [ ] `internal/gme` — `TrackInfo` via `gme_track_info` + `gme_free_info`
- [ ] `internal/gme` — `StartTrack`, `SetFade`, `Play`, `TrackEnded`, `Close`
- [ ] Add real `.nsf` fixture to `testdata/fixtures/` and unskip gme integration tests

## Phase 5: FUSE Layer

- [ ] `internal/vfs` — `Root`: scan source dir, expose real files + virtual siblings
- [ ] `internal/vfs` — `ChipDir`: `Readdir` (synthesize track filenames), `Lookup`
- [ ] `internal/vfs` — `TrackFile`: `Getattr` (report `EstimatedSize`), `Read` (serve from cache)
- [ ] `internal/vfs` — `TrackFile.Read`: serve WAV header + ID3 chunk without emulation;
      only start the emulator if the read offset reaches the `data` chunk. This prevents
      Navidrome's metadata scanner from triggering full track renders for 50+ files
      simultaneously, which would spike RAM by hundreds of MB on a cold library scan.
- [ ] `cmd/chipfs` — Wire everything together: parse flags, call `fuse.Mount`

## Phase 6: Integration & Hardening

- [ ] Docker: confirm FUSE mount works with `--cap-add SYS_ADMIN --device /dev/fuse`
- [ ] Navidrome: confirm scanner reads Artist/Album/Title correctly from virtual WAVs
- [ ] Stress test with `fsstress` (Linux kernel tool)
- [ ] Mount option: `-o default_length=180` (seconds for tracks without duration metadata)
- [ ] Mount option: `-o fade_length=8`
- [ ] Mount option: `-o cache_size_mb=256`
- [ ] RSN support (RAR containing multiple SPCs) — optional, via libarchive

## Deferred / Out of Scope for v1

- FLAC output (WAV is sufficient for Navidrome)
- N64/PSX/PS2 formats (emulation too slow for on-the-fly rendering)
- Write support (ChipFS is intentionally read-only)
- Windows support
