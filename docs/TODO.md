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

## Phase 7: Correctness & Compatibility ✓

- [x] `internal/vfs` — Thundering herd: singleflight keyed by `(sourcePath, trackIdx)`
      coalesces concurrent renders into one
- [x] `internal/wav` — `LIST INFO` RIFF chunk (INAM/IART/IPRD) alongside `id3 ` for
      compatibility with older WAV parsers
- [x] `internal/vfs` — `RealFile`: go-fuse `FileHandle` holds open fd across reads;
      `Release` closes it
- [x] `internal/vfs` — Format parser split: `buildTrackList` uses pure-Go parsers for
      mount-time scan; libgme reserved for rendering only

## Phase 8: Test Coverage & CI ✓

- [x] `internal/vfs` — `TestTrackFile_Read_RenderErrorReturnsEIO`: creates a corrupt
      NSF (valid magic, truncated body) in a temp dir; reads at PCM offset; verifies
      that gme.Open failure propagates as EIO
- [x] Smoke test — section 5 remounts with `-allow_other` and verifies the virtual
      directory is accessible
- [x] CI — `.github/workflows/ci.yml`: `go test -race ./...` on macos-latest +
      Docker smoke test on ubuntu-latest; status badge added to README

## Phase 9: Mount Options ✓

- [x] `-default_length` — play duration in seconds for tracks without embedded duration
      metadata (default 180; converts to ms in `vfs.Options.DefaultPlayMs`)
- [x] `-fade_length` — fade-out duration in seconds (default 8; converts to ms in
      `vfs.Options.DefaultFadeMs`)
- [x] `-cache_size_mb` — LRU cache capacity in MB (default 256; converts to bytes in
      `vfs.Options.CacheBytes`)

## Deferred / Out of Scope for v1

- Stress test with `fsstress` (Linux kernel tool) — useful but requires a dedicated Linux setup
- RSN support (RAR containing multiple SPCs) — optional, via libarchive
- FLAC output (WAV is sufficient for Navidrome)
- N64/PSX/PS2 formats (emulation too slow for on-the-fly rendering)
- Write support (ChipFS is intentionally read-only)
- Windows support
- Pre-built release binaries for Linux/arm64 (Raspberry Pi) — CGO dependency on
  libgme complicates cross-compilation; build on-device or use Docker buildx with
  an ARM sysroot; GitHub Actions matrix build is the right long-term solution
- Live directory watching (`fsnotify`) — detect files added/removed after mount
  and update the VFS tree without remounting; requires `sync.RWMutex` around the
  track list and handling `Lookup` for directories that didn't exist at mount time
