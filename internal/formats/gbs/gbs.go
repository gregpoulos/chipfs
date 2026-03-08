// Package gbs parses GBS (Game Boy Sound) files.
//
// GBS is the Game Boy equivalent of NSF: a compact format storing the original
// Game Boy audio code. The 0x70-byte header provides global metadata and a track
// count; there is no per-track title or duration information in the format.
package gbs

import "errors"

// ErrInvalidMagic is returned when the data does not begin with the GBS magic bytes.
var ErrInvalidMagic = errors.New("not a valid GBS file: invalid magic bytes")

// Header contains the parsed metadata from a GBS file.
type Header struct {
	Title      string
	Author     string
	Copyright  string
	TrackCount int
	FirstTrack int // 1-indexed default starting track
}

// Parse parses a GBS file from raw bytes and returns its header metadata.
func Parse(data []byte) (*Header, error) {
	// TODO: implement
	// 1. Check magic "GBS" at [0:3]
	// 2. Check version byte == 1 at [3]
	// 3. Read track_count at [4], first_track at [5]
	// 4. Read null-padded strings: title [0x10:0x30], author [0x30:0x50], copyright [0x50:0x70]
	return nil, errors.New("not implemented")
}
