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

## Known Pitfalls

**CGO preamble comments must use `//`, never `/* */`.** The CGO preamble is
delimited by a Go `/* */` block comment. Any C-style `/* ... */` inside it
contains `*/` which prematurely closes the block comment, causing opaque Go
parse errors (`non-declaration statement outside function body`, etc.).
Always use `//` line comments inside CGO preambles.

**Check the target platform's library version before writing CGO wrappers.**
macOS Homebrew and Debian bookworm often ship different library versions with
incompatible APIs. For libgme: Homebrew provides 0.6.4 (has `gme_set_fade_msecs`
and `gme_info_t.fade_length`); Debian bookworm ships 0.6.3 (neither). The
pattern for bridging version differences is a version-gated C shim in the CGO
preamble (`#if defined(LIB_VERSION) && LIB_VERSION >= 0xXXXXXX`). Confirm the
Debian package version with `apt-cache show <pkg>` before writing any CGO calls,
and run `docker build --target builder .` after writing the wrapper to catch
version errors before the rest of the implementation.

**go-fuse NodeReader requires NodeOpener to be implemented.** Without an
explicit `Open()` method, the FUSE kernel module may return EOPNOTSUPP
("Operation not supported") for all reads on a file node, even if `NodeReader`
is correctly implemented. Always implement `NodeOpener` alongside `NodeReader`.
For virtual files that manage their own cache, use `FOPEN_DIRECT_IO` to bypass
the kernel page cache; for passthrough files, use `FOPEN_KEEP_CACHE`.

**FUSE read buffers are large.** The FUSE kernel module passes read requests
with the kernel's configured `max_read` buffer (default 128 KB). Never assume
`len(dest)` in a `NodeReader.Read` call matches the logical data size being
requested. In particular: a read of offset 0 with `len(dest)=131072` on a file
whose WAV header is 150 bytes will have `off + len(dest) >> len(header)`. See
`TrackFile.Read` for the correct pattern: serve header bytes + zero-fill for
any bytes beyond the header, so clients get a full-sized response without a
short-read that some parsers treat as EOF.

## Available Skills

- `/simplify` — After completing an implementation phase, use this to review
  the code for unnecessary complexity, duplication, or quality issues.
