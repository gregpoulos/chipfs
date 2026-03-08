// Package gme provides a Go wrapper around libgme (Game Music Emu).
//
// libgme is a C++ library with a stable C API (gme.h) that accurately emulates
// the sound hardware of NES, SNES, Game Boy, Sega Genesis, and other classic
// consoles. This package wraps the C interface via CGO.
//
// # CGO Requirements
//
// This package requires libgme to be installed:
//   - macOS:   brew install game-music-emu
//   - Ubuntu:  apt install libgme-dev
//
// The CGO flags are set in a separate file (cgo.go) so that the rest of the
// package can be compiled and tested without CGO when running on systems where
// libgme is not available.
//
// # Thread Safety
//
// An Emu is not safe for concurrent use. If the same track must be served to
// multiple readers, protect the Emu with a sync.Mutex or use a single rendering
// goroutine that fills a shared cache.Buffer.
package gme

import "errors"

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
	PlayMs    int // play_length in ms; 0 means the track has no defined end
	IntroMs   int // intro_length in ms (before first loop)
	LoopMs    int // loop_length in ms
}

// Emu wraps a libgme Music_Emu handle and exposes a safe Go API.
// Call Close when done to release the underlying C resource.
type Emu struct {
	// handle *C.Music_Emu  // added when CGO is implemented
	sampleRate int
}

// Open loads a chiptune file from raw bytes and returns an Emu ready for
// playback. The format (NSF, SPC, GBS, etc.) is auto-detected by libgme.
// sampleRate is the output sample rate in Hz (typically 44100).
func Open(data []byte, sampleRate int) (*Emu, error) {
	if sampleRate <= 0 {
		return nil, ErrInvalidSampleRate
	}
	// TODO: implement CGO call:
	//   var emu *C.Music_Emu
	//   cdata := C.CBytes(data); defer C.free(cdata)
	//   if err := C.gme_open_data(cdata, C.long(len(data)), &emu, C.int(sampleRate)); err != nil { ... }
	return nil, errors.New("gme: not implemented")
}

// TrackCount returns the number of tracks in the loaded file.
func (e *Emu) TrackCount() int {
	// TODO: return int(C.gme_track_count(e.handle))
	return 0
}

// TrackInfo returns metadata for the given 0-indexed track.
func (e *Emu) TrackInfo(index int) (TrackInfo, error) {
	// TODO: implement via gme_track_info() + gme_free_info()
	return TrackInfo{}, errors.New("gme: not implemented")
}

// StartTrack prepares the emulator to render the given 0-indexed track.
// Must be called before Play.
func (e *Emu) StartTrack(index int) error {
	// TODO: implement via gme_start_track()
	return errors.New("gme: not implemented")
}

// SetFade sets the fade-out start point in milliseconds from the track start.
// The emulator will apply a linear fade after this point and signal completion
// via TrackEnded. Must be called after StartTrack.
func (e *Emu) SetFade(startMs int) {
	// TODO: implement via gme_set_fade()
}

// Play fills buf with the next len(buf) interleaved stereo int16 PCM samples.
// Returns an error if the underlying emulator faults.
func (e *Emu) Play(buf []int16) error {
	// TODO: implement via gme_play()
	return errors.New("gme: not implemented")
}

// TrackEnded reports whether the current track has reached its fade-out end.
// When true, Play will produce silence.
func (e *Emu) TrackEnded() bool {
	// TODO: return bool(C.gme_track_ended(e.handle))
	return false
}

// Close releases the libgme emulator handle. After Close, the Emu must not be used.
func (e *Emu) Close() {
	// TODO: C.gme_delete(e.handle)
}
