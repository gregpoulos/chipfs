# ChipFS — Agent Guide

ChipFS is a read-only FUSE filesystem that mounts a directory of chiptune files
(NSF, GBS, SPC) and presents each one as a virtual folder of playable WAV tracks,
making classic video game music accessible to media servers like Navidrome.

See [docs/LIVING_SPEC.md](docs/LIVING_SPEC.md) for architecture details and
[TODO.md](TODO.md) for the current implementation phase.

## Prerequisites

```bash
brew install go game-music-emu   # macOS
# apt install golang libgme-dev  # Debian/Ubuntu (for Docker/Linux)
```

Optional (for FUSE mount testing on macOS only):
```bash
brew install --cask macfuse
```

## Setup

```bash
go mod download
go test ./...   # implemented packages pass; stubs for unimplemented packages will fail
```

## TDD Workflow

This project uses red/green TDD. The cycle is:

1. Pick the next unchecked item in [TODO.md](TODO.md)
2. Run `go test ./internal/<package>/...` — confirm the test fails
3. Implement the minimum code to make it pass
4. Run `go test ./internal/<package>/...` — confirm it passes
5. Run `/simplify` to review the implementation for quality and redundancy
6. Move to the next item

Never write implementation code before a failing test exists for it.

Each format parser should have both synthetic-fixture tests (constructed in
test code) and a real-file fixture test. Real files catch spec-vs-encoder
divergence that synthetic fixtures miss — for example, real SPC files
null-terminate duration fields while the spec implies space-padding, a
difference that causes silent incorrect output rather than a test failure.

## Architecture Quick Reference

| Package | Responsibility |
|---|---|
| `internal/formats/nsf` | Parse NSF/NSFe binary headers; extract track count + metadata |
| `internal/formats/gbs` | Parse GBS binary headers |
| `internal/formats/spc` | Parse SPC ID666 tags |
| `internal/wav` | Build WAV byte slices from PCM samples; inject ID3 tags; calculate exact file sizes |
| `internal/cache` | LRU in-memory store for fully-rendered WAV tracks (keyed by path + track index) |
| `internal/gme` | CGO wrapper around libgme: open files, enumerate tracks, render PCM samples |
| `internal/vfs` | FUSE nodes (Root, ChipDir, TrackFile) using hanwen/go-fuse |
| `cmd/chipfs` | Entry point: flag parsing, FUSE mount |

## Running Tests

```bash
go test ./...                            # all unit tests
go test ./internal/formats/...           # format parsers only (no CGO needed)
go test -run TestCache ./internal/cache  # specific test
go test -v ./internal/wav/...            # verbose output
```

## Key Constraints

**CGO is required** for `internal/gme`. The `libgme` headers must be on the
include path. On macOS with Homebrew this is automatic; on Linux set:
```bash
export CGO_CFLAGS="-I/usr/include"
export CGO_LDFLAGS="-lgme"
```

**FUSE integration tests require Linux or macFUSE.** The tests in
`internal/vfs` that are marked `t.Skip("integration test: requires FUSE mount")`
run only in Docker or on a machine with macFUSE installed. All other tests run
anywhere.

**The WAV muxer's `EstimatedSize` must exactly match `Encode` output** for the
same duration and options. This invariant is critical: FUSE `getattr` reports
`EstimatedSize` before emulation begins, and any mismatch causes media servers
to truncate or reject the stream. The test `TestEstimatedSize_MatchesActualEncodeOutput`
enforces this.

## Available Skills

- `/simplify` — After completing an implementation phase, use this to review
  the code for unnecessary complexity, duplication, or quality issues.
