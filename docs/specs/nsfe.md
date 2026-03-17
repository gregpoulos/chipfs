# NSFe (NES Sound Format Extended) Specification

Source: https://www.nesdev.org/wiki/NSFe

## File Header

4-byte magic: `'N','S','F','E'` (ASCII)

All integers are little-endian throughout.

## Chunk Format

Every chunk uses this 8-byte framing header, followed by the data:

| Offset | Size | Field       | Notes                                                               |
|--------|------|-------------|---------------------------------------------------------------------|
| +0000  | 4    | Data length | DWORD; byte count of chunk data only (excludes this 8-byte header) |
| +0004  | 4    | Chunk ID    | FOURCC ASCII identifier                                             |
| +0008  | Var  | Chunk data  |                                                                     |

**Mandatory chunk rule:** If the first byte of the chunk ID is an uppercase letter (A–Z),
the chunk is mandatory. Players must reject files containing unrecognized mandatory chunks.

## Required Chunks (in order)

- **INFO** — must precede DATA
- **DATA** — raw ROM
- **NEND** — marks end of file; no chunks follow; data (if any) is ignored

## INFO Chunk (Required)

Minimum size: 9 bytes.

| Offset | Size | Field           | Notes                                              |
|--------|------|-----------------|----------------------------------------------------|
| $0000  | 2    | Load address    | $8000–FFFF, little-endian                          |
| $0002  | 2    | Init address    | $8000–FFFF, little-endian                          |
| $0004  | 2    | Play address    | $8000–FFFF, little-endian                          |
| $0006  | 1    | PAL/NTSC flags  | Bit 0 = PAL; Bit 1 = dual support                  |
| $0007  | 1    | Expansion chips | Bits 0–6: VRC6, VRC7, FDS, MMC5, N163, 5B, VT02+  |
| $0008  | 1    | Total songs     | 1-based count                                      |
| $0009  | 1    | Starting song   | 0-indexed (unlike NSF which is 1-indexed)          |

## DATA Chunk (Required)

Raw ROM bytes placed at the load address from INFO. No minimum length; maximum 1 MB usable.

## NEND Chunk (Required)

Zero-length (conventionally). Marks end of file.

## Optional Chunks

### BANK
8 bytes; initial bankswitch values (same semantics as NSF $070–$077). Missing trailing bytes
default to 0.

### RATE
Custom playback rates (overrides default NMI frequency):

| Offset | Size | Field       | Notes                 |
|--------|------|-------------|-----------------------|
| $0000  | 2    | NTSC rate   | Microseconds per call |
| $0002  | 2    | PAL rate    | Optional              |
| $0004  | 2    | Dendy rate  | Optional              |

### auth
Four null-terminated UTF-8 strings in order:
1. Game title
2. Artist
3. Copyright
4. Ripper

Missing strings default to `"<?>"`.

### tlbl
Null-terminated UTF-8 strings in track order; provides per-track names.

### taut
Null-terminated UTF-8 strings in track order; provides per-track author names.

### time
Array of 4-byte signed integers (milliseconds). One entry per track; specifies duration
before fadeout begins. **Negative = use player default.**

### fade
Array of 4-byte signed integers (milliseconds). One entry per track; specifies fadeout
duration. Zero = immediate end; negative = player default.

### plst
Ordered playlist: bytes are 0-indexed track numbers specifying play sequence.

### psfx
Sound effect tracks: bytes are 0-indexed track numbers marked as effects rather than music.

### NSF2
1 byte; NSF2 flags (equivalent to NSF header offset $7C).

### VRC7
1 byte device variant (0 = VRC7, 1 = YM2413), optionally followed by 128 or 152-byte patch set.

### text
Single null-terminated UTF-8 string with free-form descriptive text. Newlines: CR+LF or LF.

### mixe
Output device mixing levels relative to APU squares. Repeating records of 3 bytes:

| Offset | Size | Field      | Notes                                  |
|--------|------|------------|----------------------------------------|
| +0     | 1    | Device code| 0–7                                    |
| +1     | 2    | Level      | Signed 16-bit millibels vs. APU square |

Device codes: 0=APU squares, 1=APU triangle/noise/DPCM, 2=VRC6, 3=VRC7, 4=FDS, 5=MMC5,
6=N163, 7=Sunsoft 5B.

### regn
Regional support and preference:

| Offset | Size | Field            | Notes                                     |
|--------|------|------------------|-------------------------------------------|
| $0000  | 1    | Region bitfield  | Bit 0=NTSC, Bit 1=PAL, Bit 2=Dendy        |
| $0001  | 1    | Preferred region | Optional; 0=NTSC, 1=PAL, 2=Dendy          |

## String Encoding

UTF-8 is recommended for all string fields in NSFe (unlike NSF which uses ASCII/CP-932).
Track-specific metadata chunks (time, fade, tlbl, taut) should be placed after the INFO chunk.

## Key Asymmetry vs NSF

- NSF starting song ($007): **1-indexed**
- NSFe INFO starting song ($0009): **0-indexed**
