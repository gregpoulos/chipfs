# ChipFS

[![CI](https://github.com/gregpoulos/chipfs/actions/workflows/ci.yml/badge.svg)](https://github.com/gregpoulos/chipfs/actions/workflows/ci.yml)

A read-only FUSE filesystem that presents chiptune files (NES `.nsf`, Game Boy
`.gbs`, SNES `.spc`) as folders of playable WAV tracks, enabling media servers
like Navidrome to scan and stream classic video game music.

## Prerequisites

| Dependency | macOS | Debian/Ubuntu |
|---|---|---|
| Go ≥ 1.22 | `brew install go` | `apt install golang` |
| libgme | `brew install game-music-emu` | `apt install libgme-dev` |
| FUSE (optional, local mount) | `brew install --cask macfuse` | `apt install fuse3` |

## Building

```bash
git clone https://github.com/gregpoulos/chipfs
cd chipfs
go build -o chipfs ./cmd/chipfs
```

## Mounting (Linux / Raspberry Pi)

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
| `-allow_other` | `false` | Allow other users (e.g. a Navidrome Docker container) to read the mount |
| `-default_length` | `180` | Default play duration in seconds for tracks without embedded duration metadata |
| `-fade_length` | `8` | Fade-out duration in seconds |
| `-cache_size_mb` | `256` | LRU cache capacity in MB |

### Using with Navidrome in Docker

If Navidrome runs in a Docker container on the same host, pass `-allow_other`
so the container can read the FUSE mount. You will also need `user_allow_other`
enabled in `/etc/fuse.conf`:

```bash
# Once, on the host:
sudo sed -i 's/#user_allow_other/user_allow_other/' /etc/fuse.conf

# Then mount with:
./chipfs -source /path/to/chiptunes -mountpoint /mnt/chipfs -allow_other
```

In your Navidrome Docker Compose, bind-mount the chipfs directory:

```yaml
volumes:
  - /mnt/chipfs:/music:ro
```

## Behaviour Notes

**The directory tree is a static snapshot.** ChipFS scans the source directory
once at mount time. Files added to the source directory after mounting are not
visible until chipfs is restarted.

**Only regular files are exposed.** Symlinks, device nodes, and other special
files in the source directory are silently skipped.

## Running Tests

Unit tests (no FUSE or external dependencies beyond libgme):
```bash
go test ./...
```

Format parser tests only (no CGO required):
```bash
go test ./internal/formats/...
```

FUSE integration tests are marked `t.Skip` and run only in Docker or on a
machine with macFUSE installed. Use the smoke test target instead (see below).

## Manual Integration Testing

To verify the full render pipeline (parser → emulator → WAV muxer) without a
FUSE mount, use the render tool:

```bash
# Render track 1 of the Pently demo to a WAV file
go run ./cmd/render -file testdata/fixtures/pently.nsf -track 0 -out /tmp/pently_track01.wav

# Render with explicit duration and fade
go run ./cmd/render -file testdata/fixtures/pently.nsf -track 0 \
  -duration 60000 -fade 3000 -out /tmp/pently_track01.wav
```

Open the output file in any media player (QuickTime, VLC, etc.) to verify the
audio sounds correct and metadata (title, artist, album) is populated.

## Docker

### Smoke test

Verifies the virtual directory structure, track counts, WAV metadata, and
the file size invariant against the bundled fixtures:

```bash
docker build --target smoke-test -t chipfs-smoke .
docker run --rm --cap-add SYS_ADMIN --device /dev/fuse chipfs-smoke
```

### Navidrome integration test

Runs ChipFS and Navidrome together in a single container so you can verify
that Artist/Album/Title tags are read correctly by a real media server:

```bash
docker compose -f docker-compose.navidrome-test.yml up --build
```

Then open `http://localhost:4533`, create an admin account, and confirm that
the fixture files appear with correct metadata (Artist, Album, track titles).

## Supported Formats

| Format | System | Per-track metadata |
|---|---|---|
| `.nsf` | NES | Global only (title, artist, copyright) |
| `.nsfe` | NES (extended) | Per-track titles and durations |
| `.gbs` | Game Boy | Global only |
| `.spc` | SNES | Full (song title, artist, duration) |

## Limitations

- Read-only. The source directory is never modified.
- The virtual directory tree is a static snapshot of the source directory at
  mount time. Restart chipfs to pick up new files.
- N64, PS1, PS2, and later console formats are not supported — their emulation
  is too computationally expensive for real-time rendering.
- FUSE integration tests require Linux or macOS with macFUSE; unit tests run anywhere.
