# ChipFS — Concepts Guide

*This document explains the core ideas behind ChipFS in accessible language,
aimed at developers who may not be familiar with chiptune formats, FUSE filesystems,
or audio generation. It is a reference document and is not updated as the
implementation evolves.*

---

## The Problem: A Language Barrier Between Old Music and Modern Software

Video game music from the 8-bit and 16-bit eras was never stored as audio in the
traditional sense. Cartridges didn't have enough memory to hold recordings. Instead,
they stored the *sheet music* — compact program code that told the sound chip how to
produce sounds in real time. When you pressed Play on a game, the CPU was literally
running a tiny music program, 60 times per second, generating audio on the fly.

Modern rips of this music preserve that original format. An `.nsf` file (for NES)
isn't a recording of the soundtrack — it's the original music code, extracted from
the cartridge. It can contain the entire soundtrack of a game in a file smaller
than a single JPEG. Open one in a hex editor and you'll see the game's title,
artist, and a track count — say, 47 tracks — followed by raw machine code.

The problem is that almost no modern software knows what to do with an `.nsf` file.
Media servers like Navidrome expect standard audio files: MP3, FLAC, WAV. When
Navidrome scans a folder and finds `Mega_Man_2.nsf`, it either ignores it or fails.
It has no idea that file contains 24 playable songs.

---

## Why Not Just Convert Everything?

The obvious fix is to run a batch converter: turn every NSF file into a folder of
MP3s and point Navidrome at that.

This works but has real costs:

- **Storage:** A single NSF file might be 50KB. Converting its 47 tracks to MP3
  produces 150MB. If you have 500 games, a compact 25MB collection becomes 75GB.
- **Maintenance:** Update your collection, change your preferred bitrate, or fix
  a metadata error, and you re-encode everything.
- **Inflexibility:** The generated files are disconnected from their source.

The conversion approach treats the symptom instead of the underlying problem.

---

## The Solution: Lie to the Operating System (Politely)

Linux has a feature called FUSE — Filesystem in Userspace. It lets you write a
program that *pretends to be a hard drive*. When the operating system asks "what
files are in this directory?", your program intercepts that question and answers
however it wants. When the OS asks "give me the bytes of this file", your program
generates those bytes on the spot and hands them back.

The OS and every program running on top of it have no idea the "files" aren't real.
As far as they're concerned, they're just reading from disk.

ChipFS sits between Navidrome and your actual music collection and performs a
real-time translation:

- Navidrome asks: "What's in the NES folder?"
- ChipFS answers: "There's `Mega_Man_2.nsf`, and also a folder called `Mega_Man_2/`
  containing 24 `.wav` files."
- Navidrome asks: "Give me the bytes of `Mega_Man_2/01 - Flash Man.wav`."
- ChipFS boots up a NES emulator, runs the music code for Track 1, captures the
  audio output as raw numbers, wraps it in a WAV file header, and hands the bytes
  back.

No files were written to disk. The WAV file doesn't exist. ChipFS manufactured it
on demand.

---

## What Is PCM?

Sound is vibration. A microphone converts air pressure waves into a continuously
varying electrical voltage. Computers can't store a continuous signal — they can
only store numbers — so the voltage is *sampled*: measured 44,100 times per second,
with each measurement recorded as a number.

That list of numbers is **PCM — Pulse Code Modulation**. It's the most direct
representation of audio a computer can store: no compression, no encoding tricks,
just a long sequence of snapshots of a sound wave.

For a 3-minute song at CD quality (44,100 samples/second, stereo, 16-bit numbers),
that's roughly 30 million numbers taking up about 30MB.

The NES emulator doesn't record anything — it *synthesizes* audio from scratch. It
simulates the NES sound chip step by step, calculating what voltage the chip's
output pin would have had at each moment. The result is identical in structure to
PCM from a microphone: a list of numbers representing a sound wave, computed rather
than recorded.

---

## What Is a WAV File?

A WAV file is not a special audio format. It's just PCM with a label on it.

The label — called a **header** — sits at the very beginning of the file and
answers a few questions: how many samples per second, how many channels, how many
bits per sample, and how many bytes of audio data follow. After those 44 bytes of
header, the rest of the file is the raw PCM numbers.

WAV is famously simple, which is why it's useful here — there's almost nothing that
can go wrong. WAV is also the only common audio format where the file size is
mathematically exact given a known duration and sample rate, which solves a critical
FUSE problem described below.

---

## What Is a Muxer?

**Mux** is short for **multiplexer** — originally an electronics term for combining
multiple signals into one. In software, a **muxer** takes raw content (PCM samples)
and packages it into a specific file format (WAV, MP4, etc.) by adding the
appropriate headers and structure.

Think of it like a shipping department: the emulator produces the goods (PCM
numbers); the muxer puts those goods into a standardized box (the WAV format) with
a label on the outside (the header) that tells the recipient exactly what's inside.

ChipFS's WAV muxer also embeds an ID3 tag block in the WAV file — this is where
Artist, Album, Title, and Track Number are stored. Navidrome reads this tag to
populate its database.

---

## How "On the Fly" Works

When Navidrome "opens" a virtual WAV file, nothing has been generated yet. But
Navidrome doesn't receive the whole file at once — it asks for small pieces:

> "Give me bytes 0 through 4095."
> "Give me bytes 4096 through 8191."
> ...and so on.

Each request is separate. ChipFS only needs to have generated the bytes currently
being asked for.

When Navidrome asks for the first chunk, ChipFS does two things simultaneously:

1. Starts the emulator running in a background thread, generating PCM samples into
   a growing memory buffer.
2. Waits until the buffer is large enough to contain the requested bytes, then
   returns them.

While Navidrome processes those bytes, the emulator keeps running. By the time
Navidrome asks for the next chunk, the emulator has usually already generated it.
The reader and generator run in parallel; the generator stays comfortably ahead.

This is called a **producer-consumer pipeline**. The emulator is the producer,
Navidrome is the consumer, and the memory buffer is the pipeline between them. As
long as the producer stays ahead of the consumer — and for a simple NES chip running
at ~900× real time, it always does — Navidrome never has to wait.

---

## The File Size Problem

FUSE requires ChipFS to report a file's size *before* reading a single byte of
audio. This happens in a call called `getattr`, and it happens before `open`.

For WAV output, the size is mathematically exact:

```
size = (duration_seconds × sample_rate × channels × 2) + 44
```

The `2` is because each sample is a 16-bit integer (2 bytes). The `44` is the WAV
header. ChipFS calculates this formula using the track duration from the file's
metadata (or a configurable default), writes the result into the WAV header at
the start of the buffer, and reports that number to FUSE.

The header is a promise ChipFS makes upfront. The emulator then fulfills that
promise by generating exactly that many bytes before stopping.

---

## Seeking

If you drag a progress bar to the middle of a track, the player asks ChipFS for
bytes from that offset. Because emulation is state-dependent (each sample depends
on all prior samples), ChipFS cannot jump to the middle — it would have to
re-emulate from the beginning.

In practice, this is rarely a problem for two reasons:

1. **The cache.** The emulator runs far faster than real time. By the time you
   seek, the whole track has likely already been generated and stored in RAM.
   Seeking anywhere in the file is then instant.

2. **Speed.** Even if the cache is cold, re-emulating 2 minutes of NES audio takes
   well under a second. The pause is imperceptible.

The scenario where seeking might feel slow is a cold backward seek on a very
constrained system with a large cache evicted. Even then, the worst case is a
brief pause — not a failure.

---

## Why N64 and Newer Consoles Don't Work

The NES, SNES, and Game Boy have simple, purpose-built sound chips. Emulating them
is computationally cheap — a modern CPU can simulate them hundreds of times faster
than real time.

The Nintendo 64 uses a programmable DSP (the RSP) that each game programs
differently. Accurate N64 audio emulation requires emulating the entire console —
CPU, memory bus, RSP — running simultaneously. A full N64 emulator struggles to
hit real-time speed on modest hardware. PlayStation 2 is even more demanding.

If emulation can't run faster than real time, the producer-consumer model breaks
down: the emulator can't stay ahead of the reader. On-the-fly generation becomes
impossible, and pre-conversion is the only option.

ChipFS focuses on 8-bit and 16-bit hardware where the emulation speed advantage
makes the entire approach viable.
