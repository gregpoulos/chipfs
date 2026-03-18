# ChipFS: Chiptune Files as a Virtual WAV Filesystem

*2026-03-18T18:15:36Z by Showboat 0.6.1*
<!-- showboat-id: a2133970-a941-4c23-8e06-c02608aec039 -->

ChipFS is a read-only FUSE filesystem written in Go that presents chiptune files (NES .nsf, Game Boy .gbs, SNES .spc) as folders of playable WAV tracks. A media server like Navidrome pointed at the mount sees a normal music library — with correct Artist, Album, and Title tags — backed entirely by on-the-fly emulation of the original game hardware. No pre-rendered audio files are stored anywhere.

## Building

ChipFS requires Go ≥ 1.22 and libgme (the Game Music Emulator library). On macOS:

```bash
go build -o /tmp/chipfs-demo ./cmd/chipfs && echo 'Build OK'
```

```output
Build OK
```

## Test Suite

Unit tests cover every layer of the stack — format parsers, WAV muxer, LRU cache, CGO wrapper, and FUSE nodes — and run without a FUSE mount. The race detector is enabled in CI.

```bash
go test ./...
```

```output
ok  	github.com/gregpoulos/chipfs/cmd/chipfs	(cached)
?   	github.com/gregpoulos/chipfs/cmd/render	[no test files]
ok  	github.com/gregpoulos/chipfs/internal/cache	(cached)
ok  	github.com/gregpoulos/chipfs/internal/formats/gbs	(cached)
ok  	github.com/gregpoulos/chipfs/internal/formats/nsf	(cached)
ok  	github.com/gregpoulos/chipfs/internal/formats/spc	(cached)
ok  	github.com/gregpoulos/chipfs/internal/gme	(cached)
ok  	github.com/gregpoulos/chipfs/internal/vfs	(cached)
ok  	github.com/gregpoulos/chipfs/internal/wav	(cached)
```

## Render Pipeline

The `cmd/render` tool exercises the full pipeline — format parser → libgme emulator → WAV muxer — without requiring a FUSE mount. Here we render track 1 of the Super Mario Bros. NSF fixture (a real ROM music file bundled as a test fixture):

```bash
go run ./cmd/render -file testdata/fixtures/smb.nsf -track 0 -duration 10000 -fade 2000 -out /tmp/smb_track01.wav
```

```output
Rendered: Super Mario Bros. — Track 1 — track 1/18 — 10000ms → /tmp/smb_track01.wav (2.0 MB)
```

The output is a standard WAV file. Let's inspect it with ffprobe to confirm the metadata and audio properties:

```bash
ffprobe -v quiet -show_format /tmp/smb_track01.wav 2>&1 | grep -E 'format_name|duration=|size=|TAG'
```

```output
format_name=wav
duration=12.027937
size=2121944
TAG:title=Track 1
TAG:artist=Koji Kondo
TAG:album=Super Mario Bros.
TAG:track=1
```

Artist, Album, Title, and track number are embedded as both an ID3v2 RIFF chunk (read by modern parsers) and a `LIST INFO` RIFF chunk (read by older WAV parsers). The audio is 16-bit stereo PCM at 44.1 kHz — lossless, universally playable.

## File Size Invariant

One of ChipFS's trickiest requirements: FUSE's `getattr` must report the exact file size *before* any audio has been rendered, so the media server can allocate buffers correctly. ChipFS pre-calculates the precise byte count from the duration metadata and reports it as the file size. The rendered WAV must then hit that size exactly — not off by even one byte.

The `EstimatedSize` invariant is enforced by a dedicated test:

```bash
go test ./internal/vfs/ -run TestTrackFile_EstimatedSizeMatchesRenderOutput -v
```

```output
=== RUN   TestTrackFile_EstimatedSizeMatchesRenderOutput
--- PASS: TestTrackFile_EstimatedSizeMatchesRenderOutput (0.08s)
PASS
ok  	github.com/gregpoulos/chipfs/internal/vfs	(cached)
```

## Mounting (Linux / Docker)

On Linux (or in Docker with `--cap-add SYS_ADMIN --device /dev/fuse`), ChipFS mounts with:

    ./chipfs -source /path/to/chiptunes -mountpoint /mnt/chipfs

Given a source directory containing `Super Mario Bros..nsf` (18 tracks), `DuckTales.nsfe` (16 named tracks), `Castlevania.gbs`, and `Chrono Trigger.spc` (1 track), the mountpoint appears as:

    /mnt/chipfs/
      Super Mario Bros..nsf          ← original file, passthrough
      Super Mario Bros./
        Track_01.wav … Track_18.wav
      DuckTales.nsfe
      DuckTales/
        Moon Theme.wav
        African Mines.wav  …
      Castlevania.gbs
      Castlevania/
        Track_01.wav  …
      Chrono Trigger.spc
      Chrono Trigger/
        Chrono Trigger.wav

Point Navidrome's music directory at `/mnt/chipfs` and it scans and streams everything as a normal music library.

## Architecture

    formats/nsf,gbs,spc   Pure-Go parsers — extract track count and metadata at mount time (no emulation)
             │
             ▼
         internal/vfs      FUSE nodes: Root, ChipDir, TrackFile
             │               • getattr reports pre-calculated EstimatedSize
             │               • reads in the WAV header region are served from a pre-built buffer
             │               • first read into the PCM region triggers a render via singleflight
             │                 (concurrent requests for the same track coalesce into one render)
             ▼
         internal/gme      CGO wrapper around libgme — opens ROM data, emulates hardware, yields int16 PCM
             │
             ▼
         internal/wav      WAV muxer — RIFF + fmt + id3  + LIST INFO + data
             │               • EstimatedSize must exactly match Encode output (enforced by test)
             ▼
         internal/cache    LRU cache — fully-rendered WAV blobs keyed by (path, track index)
