// Package gme provides a Go wrapper around libgme (Game Music Emu).
//
// libgme is a C++ library with a stable C API (gme.h) that accurately emulates
// the sound hardware of NES, SNES, Game Boy, and other classic consoles. This
// package wraps the C interface via CGO.
//
// # CGO Requirements
//
// This package requires libgme to be installed:
//   - macOS (Apple Silicon): brew install game-music-emu
//   - macOS (Intel):         brew install game-music-emu
//   - Ubuntu/Debian:         apt install libgme-dev
//
// # Thread Safety
//
// An Emu is not safe for concurrent use. If the same track must be served to
// multiple readers, protect it with a sync.Mutex or use a single rendering
// goroutine that writes into a shared cache entry.
package gme

/*
#cgo darwin CFLAGS: -I/usr/local/include -I/opt/homebrew/include
#cgo darwin LDFLAGS: -L/usr/local/lib -L/opt/homebrew/lib -lgme
#cgo linux LDFLAGS: -lgme
#include <gme/gme.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// ErrInvalidSampleRate is returned by Open when sampleRate is zero or negative.
var ErrInvalidSampleRate = errors.New("gme: sample rate must be positive")

// TrackInfo holds per-track metadata returned by libgme's gme_track_info().
type TrackInfo struct {
	Title     string
	Game      string
	Author    string
	Copyright string
	System    string
	Comment   string
	PlayMs    int // play_length from libgme: intro+loop×2, or 150000 if unknown
	FadeMs    int // fade_length from file metadata; -1 if not specified
	IntroMs   int // length of intro before first loop (-1 if unknown)
	LoopMs    int // length of one loop (-1 if unknown)
}

// Emu wraps a libgme Music_Emu handle and exposes a safe Go API.
// Call Close when done to release the underlying C resource.
type Emu struct {
	handle     *C.Music_Emu
	sampleRate int
}

// Open loads a chiptune file from raw bytes and returns an Emu ready for
// playback. The format (NSF, NSFe, SPC, GBS, etc.) is auto-detected by libgme.
// sampleRate is the output sample rate in Hz (typically 44100).
func Open(data []byte, sampleRate int) (*Emu, error) {
	if sampleRate <= 0 {
		return nil, ErrInvalidSampleRate
	}
	cdata := C.CBytes(data)
	defer C.free(cdata)

	var handle *C.Music_Emu
	if cerr := C.gme_open_data(cdata, C.long(len(data)), &handle, C.int(sampleRate)); cerr != nil {
		return nil, fmt.Errorf("gme: %s", C.GoString(cerr))
	}
	return &Emu{handle: handle, sampleRate: sampleRate}, nil
}

// TrackCount returns the number of tracks in the loaded file.
func (e *Emu) TrackCount() int {
	return int(C.gme_track_count(e.handle))
}

// TrackInfo returns metadata for the given 0-indexed track.
func (e *Emu) TrackInfo(index int) (TrackInfo, error) {
	var info *C.gme_info_t
	if cerr := C.gme_track_info(e.handle, &info, C.int(index)); cerr != nil {
		return TrackInfo{}, fmt.Errorf("gme: %s", C.GoString(cerr))
	}
	defer C.gme_free_info(info)

	return TrackInfo{
		Title:     C.GoString(info.song),
		Game:      C.GoString(info.game),
		Author:    C.GoString(info.author),
		Copyright: C.GoString(info.copyright),
		System:    C.GoString(info.system),
		Comment:   C.GoString(info.comment),
		PlayMs:    int(info.play_length),
		FadeMs:    int(info.fade_length),
		IntroMs:   int(info.intro_length),
		LoopMs:    int(info.loop_length),
	}, nil
}

// StartTrack prepares the emulator to render the given 0-indexed track.
// Must be called before Play.
func (e *Emu) StartTrack(index int) error {
	if cerr := C.gme_start_track(e.handle, C.int(index)); cerr != nil {
		return fmt.Errorf("gme: %s", C.GoString(cerr))
	}
	return nil
}

// SetFade sets the fade-out start point and duration. The emulator applies a
// linear fade from startMs and signals completion via TrackEnded once
// startMs+fadeLengthMs of audio has been rendered.
func (e *Emu) SetFade(startMs, fadeLengthMs int) {
	C.gme_set_fade_msecs(e.handle, C.int(startMs), C.int(fadeLengthMs))
}

// Play fills buf with the next len(buf) interleaved stereo int16 PCM samples.
// Returns an error if the underlying emulator faults.
func (e *Emu) Play(buf []int16) error {
	if len(buf) == 0 {
		return nil
	}
	if cerr := C.gme_play(e.handle, C.int(len(buf)), (*C.short)(unsafe.Pointer(&buf[0]))); cerr != nil {
		return fmt.Errorf("gme: %s", C.GoString(cerr))
	}
	return nil
}

// TrackEnded reports whether the current track has reached its fade-out end.
// When true, Play will produce silence.
func (e *Emu) TrackEnded() bool {
	return C.gme_track_ended(e.handle) != 0
}

// Close releases the libgme emulator handle. After Close, the Emu must not be used.
func (e *Emu) Close() {
	C.gme_delete(e.handle)
}
