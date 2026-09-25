# ChipFS — Implementation TODO

Open work and deferred ideas. Remove items once done.

Phases 1–9 (parsers, WAV muxer, cache, libgme wrapper, FUSE layer, Docker
smoke test, hardening, CI, mount options) are complete; see git history.

## Phase 10: Cover Art Passthrough

- [ ] `internal/vfs` — `ChipDir`: expose `cover.jpg` / `cover.png` / `folder.jpg`
      from the source directory as passthrough `RealFile` nodes alongside the WAV tracks
- [ ] `internal/vfs` — `Root`: likewise expose cover art files that sit next to a
      chiptune file, scoped to the `ChipDir` virtual folder for that file
- [ ] Smoke test — verify cover art file is present and readable in mounted virtual dir
- [ ] Confirm Navidrome picks up `cover.jpg` from the virtual album folder

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
