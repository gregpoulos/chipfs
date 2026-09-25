# ChipFS — Agent Guide

ChipFS is a read-only FUSE filesystem that mounts a directory of chiptune files
(NSF, GBS, SPC) and presents each one as a virtual folder of playable WAV tracks,
making classic video game music accessible to media servers like Navidrome.

See [docs/LIVING_SPEC.md](docs/LIVING_SPEC.md) for architecture details and
[docs/TODO.md](docs/TODO.md) for the current implementation phase.

## TDD Workflow

Pick the next unchecked item in [docs/TODO.md](docs/TODO.md), write a failing test, implement the minimum to pass it, then run `/simplify`. Never write implementation code before a failing test exists.

Each format parser needs both synthetic-fixture tests and a real-file fixture test. Real files catch spec-vs-encoder divergence that synthetic fixtures miss — for example, real SPC files null-terminate duration fields while the spec implies space-padding.

## Architecture Quick Reference

| Package | Responsibility |
|---|---|
| `internal/formats/nsf` | Parse NSF/NSFe binary headers; extract track count + metadata |
| `internal/formats/gbs` | Parse GBS binary headers |
| `internal/formats/spc` | Parse SPC ID666 tags |
| `internal/wav` | Build WAV byte slices from PCM samples; inject ID3 tags; calculate exact file sizes |
| `internal/cache` | LRU in-memory store for fully-rendered WAV tracks (keyed by path + track index) |
| `internal/gme` | CGO wrapper around libgme: open files, render PCM samples |
| `internal/vfs` | FUSE nodes (Root, ChipDir, TrackFile) using hanwen/go-fuse |
| `cmd/chipfs` | Entry point: flag parsing, FUSE mount |
| `cmd/render` | Dev tool: renders a single track to a WAV file without a FUSE mount |

## Key Constraints

**CGO is required** for `internal/gme` (libgme headers and library installed;
see README).

**FUSE behavior is tested only by the Docker smoke test**
(`docker build --target smoke-test`), which needs a FUSE-capable Docker host and
is not run by GitHub CI. Unit tests run anywhere.

**The WAV muxer's `EstimatedSize` must exactly match `Encode` output** for the
same duration and options. This invariant is critical: FUSE `getattr` reports
`EstimatedSize` before emulation begins, and any mismatch causes media servers
to truncate or reject the stream. All three of `Encode`, `HeaderBytes`, and
`EstimatedSize` derive from one header builder and `SampleCount`; keep it that
way. `TestEstimatedSize_MatchesEncode` guards it.

**libgme versions differ across build targets:** Homebrew ships 0.6.4; Debian
bookworm (`builder`/`runtime` images) and Alpine (`navidrome-test` image) ship
0.6.3. Bridge API differences with a version-gated shim in the `gme.go` CGO
preamble, and after changing CGO calls run
`docker build --target builder .` and `docker build --target navidrome-test .`.

## Available Skills

- `/simplify` — After completing an implementation phase, use this to review
  the code for unnecessary complexity, duplication, or quality issues.
