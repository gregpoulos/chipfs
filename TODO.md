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
- [x] `internal/vfs` — `TrackFile.Read`: serve WAV header + ID3 chunk without emulation;
      only start the emulator if the read offset reaches the `data` chunk.
- [x] `cmd/chipfs` — Wire everything together: parse flags, call `fuse.Mount`, SIGINT/SIGTERM handling

## Phase 6: Integration & Hardening (current)

- [x] Dockerfile — multi-stage build (builder + runtime + smoke-test targets)
- [x] `scripts/smoke-test.sh` — verifies virtual dir structure, track counts, WAV metadata, and file size invariant inside Docker
- [x] Run smoke test: `docker build --target smoke-test -t chipfs-smoke . && docker run --rm --cap-add SYS_ADMIN --device /dev/fuse chipfs-smoke`
- [ ] Navidrome: confirm scanner reads Artist/Album/Title correctly from virtual WAVs
- [ ] Stress test with `fsstress` (Linux kernel tool)
- [ ] Mount option: `-default_length` (seconds for tracks without duration metadata)
- [ ] Mount option: `-fade_length`
- [ ] Mount option: `-cache_size_mb`
- [ ] RSN support (RAR containing multiple SPCs) — optional, via libarchive

## Phase 7: Portfolio Hardening

- [ ] CI — GitHub Actions: `go test -race ./...` on macOS + Docker smoke test on ubuntu-latest; add status badge to README
- [x] `internal/vfs` — Add `defer/recover` in `TrackFile.Read` to catch panics from libgme on corrupt files and return `syscall.EIO` instead of crashing chipfs
- [x] `internal/vfs` — Fix `RealFile.Read`: replace `os.ReadFile` with `os.Open` + `ReadAt` so a partial read doesn't allocate the whole file
- [ ] `internal/vfs` — Resolve the Go format parsers vs. libgme split: either wire `internal/formats/*` into `buildTrackList` as the initial directory scan (their original purpose), or document clearly why libgme is used for everything and what the Go parsers are for
- [ ] `internal/vfs` — Thundering herd: concurrent cache misses for the same track trigger duplicate renders; use `golang.org/x/sync/singleflight` keyed by `(sourcePath, trackIdx)` to coalesce concurrent renders into one
- [x] `internal/vfs` — Log the recovered value in `TrackFile.Read`'s `defer/recover` block (currently panics are silently swallowed as EIO with no diagnostic output)
- [x] `buildTrackList` — Cap `durationMs` to a sane maximum (e.g., 20 minutes) to prevent a malformed file from claiming a huge duration, which would spike memory and wrap the `uint32` RIFF size field

## Deferred / Out of Scope for v1

- FLAC output (WAV is sufficient for Navidrome)
- N64/PSX/PS2 formats (emulation too slow for on-the-fly rendering)
- Write support (ChipFS is intentionally read-only)
- Windows support
