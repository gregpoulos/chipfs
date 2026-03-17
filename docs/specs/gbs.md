# GBS (Game Boy Sound) File Format Specification

Source: libgme source, OC ReMix specification

## Overview

GBS is a ripped Game Boy sound format analogous to NSF (NES) and PSID (C64). It bundles
GB CPU code and audio data with a fixed 112-byte header.

- File extension: `.gbs`
- Magic bytes: `"GBS"` (3 ASCII bytes: 0x47 0x42 0x53)
- Version: 1 (byte at offset 0x03)

## Header Fields (total header size: 112 bytes / 0x70)

| Offset | Size | Field        | Notes                                                                |
|--------|------|--------------|----------------------------------------------------------------------|
| 0x00   | 3    | tag          | ASCII "GBS" — file identifier                                        |
| 0x03   | 1    | vers         | Version, always 1                                                    |
| 0x04   | 1    | track_count  | Number of tracks, range 1–255                                        |
| 0x05   | 1    | first_track  | 1-based index of the first/default track, usually 1                  |
| 0x06   | 2    | load_addr    | Load address for code/data, little-endian, range $0400–$7FFF         |
| 0x08   | 2    | init_addr    | Address of init routine, little-endian                               |
| 0x0A   | 2    | play_addr    | Address of play (frame) routine, little-endian                       |
| 0x0C   | 2    | stack_ptr    | Initial stack pointer, little-endian, default $FFFE                  |
| 0x0E   | 1    | timer_modulo | TMA register value (GB timer modulo)                                 |
| 0x0F   | 1    | timer_mode   | TAC register value; bit 7 = GBC double-speed CPU clock               |
| 0x10   | 32   | game         | Game/title string, null-padded (not necessarily null-terminated)     |
| 0x30   | 32   | author       | Composer/author string, null-padded                                  |
| 0x50   | 32   | copyright    | Copyright string, null-padded                                        |
| 0x70   | var  | (code/data)  | ROM code and data, loaded at load_addr                               |

## Metadata Storage Notes

- All 3 string fields (game, author, copyright) are fixed 32-byte null-padded fields. They
  are not guaranteed to be null-terminated if the string fills all 32 bytes; parsers should
  treat all 32 bytes as valid characters if no null byte is found.
- All 2-byte address/pointer fields are little-endian.
- Unknown/unused bytes should be zero.

## Timer Interrupt Rate

The play routine call rate is determined by the timer fields:

```
If timer_mode == 0:
    rate = 59.7 Hz  (standard V-blank)
Else:
    counter_rate = one of {4096, 262144, 65536, 16384} Hz selected by timer_mode bits [1:0]
    call_rate = counter_rate / (256 - timer_modulo)
```

## Game Boy Color Support

Bit 7 of timer_mode (TAC): when set, the emulator uses the GBC double-speed CPU clock (2x normal).

## Key Constants (from libgme)

- header_size = 112 (0x70)
- joypad_addr = 0xFF00
- ram_addr    = 0xA000
- bank_size   = 0x4000 (16 KB ROM bank)
