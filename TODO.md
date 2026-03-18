# ChipFS — Implementation TODO

This file tracks the current state of implementation. Update it as phases complete.

## Phase 1: Format Parsers ✓

- [x] `internal/formats/nsf` — NSF header parser (magic, track count, global metadata)
- [x] `internal/formats/nsf` — NSFe extension chunk parser (tlbl, time, fade, auth)
- [x] `internal/formats/gbs` — GBS header parser
- [x] `internal/formats/spc` — SPC ID666 tag parser (text and binary format)

## Phase 2: WAV Muxer ✓

- [x] `internal/wav` — RIFF WAV header construction
- [x] `internal/wav` — ID3v2 tag building (TIT2, TPE1, TALB, TRCK, TYER frames)
- [x] `internal/wav` — Embed ID3v2 as RIFF `id3 ` chunk
- [x] `internal/wav` — `EstimatedSize` must exactly match `Encode` output

## Phase 3: Track Cache ✓

- [x] `internal/cache` — LRU eviction using `container/list` + `map`
- [x] `internal/cache` — Thread-safe Get/Set
- [x] `internal/cache` — Byte-accurate capacity tracking

## Phase 4: libgme CGO Wrapper ✓

- [x] `internal/gme` — CGO import block with correct Homebrew/apt flags
- [x] `internal/gme` — `Open` via `gme_open_data`
- [x] `internal/gme` — `TrackInfo` via `gme_track_info` + `gme_free_info`
- [x] `internal/gme` — `StartTrack`, `SetFade`, `Play`, `TrackEnded`, `Close`
- [x] Add real `.nsf` fixture to `testdata/fixtures/` and unskip gme integration tests

## Phase 5: FUSE Layer ✓

- [x] `internal/vfs` — `Root`: scan source dir, expose real files + virtual siblings
- [x] `internal/vfs` — `ChipDir`: `OnAdd` populates TrackFile children; `Readdir`/`Lookup` handled by go-fuse tree
- [x] `internal/vfs` — `TrackFile`: `Getattr` (report `EstimatedSize`), `Read` (serve from cache)
- [x] `internal/vfs` — `TrackFile.Read`: serve WAV header + ID3 chunk without emulation; only start the emulator if the read offset reaches the `data` chunk
- [x] `cmd/chipfs` — Wire everything together: parse flags, call `fuse.Mount`, SIGINT/SIGTERM handling

## Phase 6: Integration & Hardening ✓

- [x] Dockerfile — multi-stage build (builder + runtime + smoke-test + navidrome-test targets)
- [x] `scripts/smoke-test.sh` — verifies virtual dir structure, track counts, WAV metadata, and file size invariant inside Docker
- [x] Navidrome: confirmed scanner reads Artist/Album/Title correctly from virtual WAVs
- [x] Mount option: `-allow_other` (allow other users/containers to access the mount)

## Phase 7: Correctness & Compatibility

These items affect real behaviour under load or with non-taglib media players.
Do these before Phase 8.

- [ ] `internal/vfs` — Thundering herd: concurrent cache misses for the same track trigger
      duplicate renders; use `golang.org/x/sync/singleflight` keyed by `(sourcePath, trackIdx)`
      to coalesce concurrent renders into one
- [ ] `internal/wav` — Add `LIST INFO` RIFF chunk (INAM/IART/IPRD subchunks) alongside the
      existing `id3 ` chunk for compatibility with older WAV parsers (reviewer confirmed both
      coexist fine; affects what players outside the taglib ecosystem can read)
- [ ] `internal/vfs` — `RealFile.Read` opens and closes the file on every FUSE read call; use
      go-fuse's `FileHandle` to hold an open fd across reads and close it in `Release`
- [ ] `internal/vfs` — Resolve the Go format parsers vs. libgme split: wire `internal/formats/*`
      into `buildTrackList` for the initial directory scan (their original purpose), leaving
      libgme only for rendering

## Phase 8: Test Coverage & CI

These don't change runtime behaviour but close gaps in the safety net.

- [ ] `internal/vfs` — Replace the artificial `estimatedSize=-1` panic trigger in
      `TestTrackFile_Read_PanicReturnsEIO` with a corrupt NSF fixture so the test exercises
      the real failure path (libgme panicking on bad input)
- [ ] Smoke test — add a `-allow_other` invocation path so the flag has integration-level
      coverage beyond the unit test for flag parsing
- [ ] CI — GitHub Actions: `go test -race ./...` on macOS + Docker smoke test on ubuntu-latest;
      add status badge to README

## Phase 9: Mount Options

User-facing knobs. Implement in any order; each is self-contained.

- [ ] `-default_length` — play duration in seconds for tracks without embedded duration metadata
- [ ] `-fade_length` — fade-out length in seconds (currently hardcoded to 8 s)
- [ ] `-cache_size_mb` — LRU cache capacity (default 256 MB; reviewer suggests 128 MB is
      sufficient for most home NAS deployments)

## Deferred / Out of Scope for v1

- Stress test with `fsstress` (Linux kernel tool) — useful but requires a dedicated Linux setup
- RSN support (RAR containing multiple SPCs) — optional, via libarchive
- FLAC output (WAV is sufficient for Navidrome)
- N64/PSX/PS2 formats (emulation too slow for on-the-fly rendering)
- Write support (ChipFS is intentionally read-only)
- Windows support
