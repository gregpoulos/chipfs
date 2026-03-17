# NSF (NES Sound Format) Specification

Source: https://www.nesdev.org/wiki/NSF

## File Magic and Version

Files begin with the five-byte signature: `'N','E','S','M',$1A`

Version byte at offset $005: `$01` = NSF, `$02` = NSF2.

## Header Structure

All multi-byte values are little-endian.

| Offset | Size | Field                | Notes                                        |
|--------|------|----------------------|----------------------------------------------|
| $000   | 5    | Magic signature      | "NESM" + $1A                                 |
| $005   | 1    | Version              | $01 or $02                                   |
| $006   | 1    | Total songs          | Count from 1 upward                          |
| $007   | 1    | Starting song        | Default track (1-indexed)                    |
| $008   | 2    | Load address         | $8000–FFFF range                             |
| $00A   | 2    | Init routine address | $8000–FFFF range                             |
| $00C   | 2    | Play routine address | $8000–FFFF range                             |
| $00E   | 32   | Song title           | Null-terminated ASCII string                 |
| $02E   | 32   | Artist name          | Null-terminated ASCII string                 |
| $04E   | 32   | Copyright info       | Null-terminated ASCII string                 |
| $06E   | 2    | NTSC play speed      | Microseconds per call (1/1,000,000 sec)      |
| $070   | 8    | Bankswitch init      | Bank numbers for $8000–FFFF regions          |
| $078   | 2    | PAL play speed       | Microseconds per call                        |
| $07A   | 1    | PAL/NTSC flags       | Bit 0: PAL; Bit 1: dual mode                 |
| $07B   | 1    | Expansion chip flags | Bits indicate enabled sound chips            |
| $07C   | 1    | Reserved             | NSF2 use                                     |
| $07D   | 3    | Program data length  | 24-bit little-endian size (0 = to EOF)       |
| $080+  | Var  | Music code/data      | Actual program content                       |

## String Encoding

Metadata fields (title, artist, copyright) use plain ASCII. Some rips use Windows CP-932
(Shift-JIS) for Japanese text. Fields must be null-terminated; max 31 usable characters.
Unknown fields should contain `"<?>"`.

## PAL/NTSC Flag Byte ($07A)

| Bit | Meaning                |
|-----|------------------------|
| 0   | 0 = NTSC, 1 = PAL      |
| 1   | 1 = dual PAL/NTSC mode |

## Expansion Chip Flags ($07B)

| Bit | Chip             |
|-----|------------------|
| 0   | VRC6 audio       |
| 1   | VRC7 audio       |
| 2   | FDS audio        |
| 3   | MMC5 audio       |
| 4   | Namco 163 audio  |
| 5   | Sunsoft 5B audio |
| 6   | VT02+ audio      |
| 7   | Reserved (must be 0) |

## Bankswitch Configuration ($070–$077)

If all 8 bytes are $00, no bankswitching. Otherwise the 6502 address space is divided into
eight 4KB banks controlled by writes to $5FF8–$5FFF:

| Header Byte | Address Range | Control Register |
|-------------|---------------|------------------|
| $070        | $8000–8FFF    | $5FF8            |
| $071        | $9000–9FFF    | $5FF9            |
| $072        | $A000–AFFF    | $5FFA            |
| $073        | $B000–BFFF    | $5FFB            |
| $074        | $C000–CFFF    | $5FFC            |
| $075        | $D000–DFFF    | $5FFD            |
| $076        | $E000–EFFF    | $5FFE            |
| $077        | $F000–FFFF    | $5FFF            |

Initial ROM padding: `load_address AND $0FFF` bytes.

## Initialization Sequence

1. Zero RAM at $0000–$07FF and $6000–$7FFF
2. Clear sound registers ($4000–$4013); write $00 then $0F to $4015
3. Set frame counter to 4-step mode (write $40 to $4017)
4. Load bankswitch values if applicable
5. Place desired song number (0-indexed) in accumulator A
6. Set X: 0 = NTSC, 1 = PAL
7. Call INIT routine (Y and flags are undefined on entry)

## Playback Rate

`rate (Hz) = 1,000,000 / speed_value`

Common values: 16666 ($411A), 16639 ($40FF, NTSC), 19997 ($4E1D, PAL). After INIT returns
with RTS, call PLAY at the specified interval.
