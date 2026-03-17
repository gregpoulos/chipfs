# SPC File Format Specification (v0.31)

Sources:
- https://wiki.superfamicom.org/spc-and-rsn-file-format
- https://wiki.superfamicom.org/id666-format
- SPCFormat_031.txt (https://github.com/ullenius/spctag/blob/master/SPCFormat_031.txt)
- libid666 C library (https://github.com/jprjr/libid666)

## Header Layout

```
Offset  Size  Description
------  ----  -----------
00000h   33   File header "SNES-SPC700 Sound File Data v0.30"
00021h    2   0x1A, 0x1A  (fixed)
00023h    1   0x1A (= 26) = header contains ID666 information
              0x1B (= 27) = header contains no ID666 tag
00024h    1   Version minor (e.g. 30)

SPC700 Registers:
00025h    2   PC
00027h    1   A
00028h    1   X
00029h    1   Y
0002Ah    1   PSW
0002Bh    1   SP (lower byte)
0002Ch    2   reserved

00100h  65536  64KB RAM
10100h   128   DSP Registers
10180h    64   unused
101C0h    64   Extra RAM
10200h         Extended ID666 (xid6 RIFF chunk, optional)
```

**Note:** Byte 0x23 only signals whether an ID666 tag is present at all. It does **not**
distinguish text format from binary format.

---

## ID666 Tag — TEXT Format (0x2E–0x00FF)

```
Offset  Size  Field
------  ----  -----
0002Eh   32   Song title             (null-terminated ASCII string)
0004Eh   32   Game title             (null-terminated ASCII string)
0006Eh   16   Name of dumper         (null-terminated ASCII string)
0007Eh   32   Comments               (null-terminated ASCII string)
0009Eh   11   Date SPC was dumped    (ASCII "MM/DD/YYYY")
000A9h    3   Play duration          (ASCII decimal string, SECONDS, e.g. "120")
000ACh    5   Fade length            (ASCII decimal string, MILLISECONDS, e.g. "8000 ")
000B1h   32   Artist of song         (null-terminated ASCII string)
000D1h    1   Default channel disables (0=enable, 1=disable, bitmask)
000D2h    1   Emulator used to dump:  0=unknown, 1=ZSNES, 2=Snes9x
000D3h   45   reserved (all 0x00)
```

---

## ID666 Tag — BINARY Format (0x2E–0x00FF)

```
Offset  Size  Field
------  ----  -----
0002Eh   32   Song title             (null-terminated ASCII string)
0004Eh   32   Game title             (null-terminated ASCII string)
0006Eh   16   Name of dumper         (null-terminated ASCII string)
0007Eh   32   Comments               (null-terminated ASCII string)
0009Eh    1   Date: Day   (1–31, binary integer)
0009Fh    1   Date: Month (1–12, binary integer)
000A0h    2   Date: Year  (1–9999, binary integer, little-endian)
000A2h    7   unused
000A9h    3   Play duration          (24-bit LITTLE-ENDIAN INTEGER, SECONDS)
000ACh    4   Fade length            (32-bit LITTLE-ENDIAN INTEGER, MILLISECONDS)
000B0h   32   Artist of song         (null-terminated ASCII string)
000D0h    1   Default channel disables (bitmask)
000D1h    1   Emulator used to dump  (same codes as text format)
000D2h   46   reserved (all 0x00)
```

---

## Text vs Binary Format Detection

The spec addendum states explicitly: **"Detecting the format of the SPC file (binary or
text) can be a bit tricky, since fields might contain ambiguous values and there's no
format indicator."**

Byte 0x23 only signals ID666 presence (0x1A=present, 0x1B=absent); it does not distinguish
text from binary.

### Recommended heuristics (in order of reliability)

1. **Date field slash check (most reliable):** In text format, offset 0x9E holds an 11-byte
   ASCII string like `"01/23/1996"`. Check if `data[0xA0] == '/'` (the slash between
   month and day). If yes → text format. If no → likely binary.

2. **Duration field ASCII check:** If `data[0xA9]` is an ASCII digit (`0x30`–`0x39`) →
   infer text format. Edge case: binary files with 48–57 second durations would
   false-positive here (accepted limitation in practice).

3. **Emulator byte position:** Binary format emulator byte is at 0xD1; text format is at
   0xD2. A value of 1 (ZSNES) or 2 (Snes9x) at 0xD1 suggests binary.

4. **Producer behavior:** ZSNES (Win32) produces binary SPC files; Snes9x produces text.

---

## Duration Field Encoding — Detail

### TEXT format
- **0xA9** (3 bytes): ASCII decimal string, seconds before fade. e.g. `"120"` or `"61 "`.
  Real files frequently null-terminate (`"61\x00"`) rather than space-pad.
- **0xAC** (5 bytes): ASCII decimal string, fade length in milliseconds. e.g. `"8000 "`.

### BINARY format
- **0xA9** (3 bytes): **24-bit little-endian integer**, seconds before fade. To read: copy 3
  bytes into a 4-byte buffer (zero-padded high byte) and interpret as uint32-LE.
- **0xAC** (4 bytes): **32-bit little-endian integer**, fade length in milliseconds.

libid666 reference implementation:
```c
// Binary play duration
mem_cpy(t_tmp, &data[0xA9], 3);
id6->len = (int)(unpack_uint32le(t_tmp) * 64000);  // converts seconds to 1/64000ths

// Binary fade duration
mem_cpy(t_tmp, &data[0xAC], 4);
id6->fade = (int)(unpack_uint32le(t_tmp) * 64);    // converts ms to 1/64000ths
```

---

## Key Structural Differences Between Formats

| Field          | Text offset | Text size | Binary offset | Binary size | Text encoding       | Binary encoding         |
|----------------|-------------|-----------|---------------|-------------|---------------------|-------------------------|
| Date           | 0x9E        | 11        | 0x9E–0xA1     | 4 total     | ASCII "MM/DD/YYYY"  | day/month/year integers |
| Unused         | —           | —         | 0xA2          | 7           | —                   | zero bytes              |
| Play duration  | 0xA9        | 3         | 0xA9          | 3           | ASCII decimal secs  | 24-bit LE int, seconds  |
| Fade length    | 0xAC        | **5**     | 0xAC          | **4**       | ASCII decimal ms    | 32-bit LE int, ms       |
| Artist         | **0xB1**    | 32        | **0xB0**      | 32          | null-term ASCII     | null-term ASCII         |
| Ch. disables   | **0xD1**    | 1         | **0xD0**      | 1           | bitmask             | bitmask                 |
| Emulator       | **0xD2**    | 1         | **0xD1**      | 1           | numeric code        | numeric code            |

The artist, channel disables, and emulator fields are all shifted by one byte between
formats because the fade field is 5 bytes in text vs 4 bytes in binary.
