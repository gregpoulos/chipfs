# ChipFS

A read-only FUSE filesystem that presents chiptune files (NES `.nsf`, Game Boy
`.gbs`, SNES `.spc`) as folders of playable WAV tracks, enabling media servers
like Navidrome to scan and stream classic video game music.

## Prerequisites

| Dependency | macOS | Debian/Ubuntu |
|---|---|---|
| Go ≥ 1.21 | `brew install go` | `apt install golang` |
| libgme | `brew install game-music-emu` | `apt install libgme-dev` |
| FUSE (optional, local mount) | `brew install --cask macfuse` | `apt install fuse3` |

## Building

```bash
git clone https://github.com/gregpoulos/chipfs
cd chipfs
go build -o chipfs ./cmd/chipfs
```

## Mounting (Linux)

```bash
mkdir /mnt/chipfs
./chipfs -source /path/to/your/chiptunes -mountpoint /mnt/chipfs
```

Point Navidrome's music directory at `/mnt/chipfs`.

To unmount:
```bash
fusermount -u /mnt/chipfs
```

## Mounting (macOS with macFUSE)

Same as Linux, but use `umount /mnt/chipfs` to unmount.

macOS support is best-effort. The primary deployment target is Linux.

## Mount Options

| Option | Default | Description |
|---|---|---|
| `-source` | *(required)* | Directory containing your chiptune files |
| `-mountpoint` | *(required)* | Empty directory to mount the virtual filesystem |

Additional options (`-default_length`, `-fade_length`, `-cache_size_mb`) are
planned for Phase 6 but not yet implemented.

## Running Tests

Unit tests (no FUSE or external dependencies beyond libgme):
```bash
go test ./...
```

Format parser tests only (no CGO required):
```bash
go test ./internal/formats/...
```

Integration tests (requires Linux or macOS with macFUSE):
```bash
go test -tags integration ./...
```

## Manual Integration Testing

To verify the full render pipeline (parser → emulator → WAV muxer) without a
FUSE mount, use the render tool:

```bash
# Render track 1 of Super Mario Bros. to a WAV file
go run ./cmd/render -file testdata/fixtures/smb.nsf -track 0 -out /tmp/smb_track01.wav

# Render with explicit duration and fade
go run ./cmd/render -file testdata/fixtures/smb.nsf -track 0 \
  -duration 60000 -fade 3000 -out /tmp/smb_track01.wav
```

Open the output file in any media player (QuickTime, VLC, etc.) to verify the
audio sounds correct and metadata (title, artist, album) is populated.

## Docker (Linux, for CI or NAS deployment)

```bash
docker build -t chipfs .
docker run --rm \
  --cap-add SYS_ADMIN \
  --device /dev/fuse \
  --security-opt apparmor:unconfined \
  -v /your/chiptunes:/source:ro \
  -v /your/mountpoint:/mnt/chipfs:shared \
  chipfs -source /source -mountpoint /mnt/chipfs
```

## Supported Formats

| Format | System | Per-track metadata |
|---|---|---|
| `.nsf` | NES | Global only (title, artist, copyright) |
| `.nsfe` | NES (extended) | Per-track titles and durations |
| `.gbs` | Game Boy | Global only |
| `.spc` | SNES | Full (song title, artist, duration) |

## Limitations

- Read-only. The source directory is never modified.
- N64, PS1, PS2, and later console formats are not supported — their emulation
  is too computationally expensive for real-time rendering.
- FUSE integration tests require Linux or macOS with macFUSE; unit tests run anywhere.
