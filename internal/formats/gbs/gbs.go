// Package gbs parses GBS (Game Boy Sound) files.
//
// GBS is the Game Boy equivalent of NSF: a compact format storing the original
// Game Boy audio code. The 0x70-byte header provides global metadata and a track
// count; there is no per-track title or duration information in the format.
package gbs

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrInvalidMagic is returned when the data does not begin with the GBS magic bytes.
var ErrInvalidMagic = errors.New("not a valid GBS file: invalid magic bytes")

// magic is the three-byte sequence that begins every GBS file: "GBS".
var magic = []byte{0x47, 0x42, 0x53}

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
	if len(data) < 3 {
		return nil, fmt.Errorf("gbs: data too short to contain magic bytes")
	}
	if !bytes.Equal(data[0:3], magic) {
		return nil, ErrInvalidMagic
	}
	if len(data) < 0x70 {
		return nil, fmt.Errorf("gbs: header truncated: need %d bytes, got %d", 0x70, len(data))
	}

	return &Header{
		TrackCount: int(data[4]),
		FirstTrack: int(data[5]),
		Title:      nullPaddedString(data[0x10:0x30]),
		Author:     nullPaddedString(data[0x30:0x50]),
		Copyright:  nullPaddedString(data[0x50:0x70]),
	}, nil
}

// nullPaddedString converts a fixed-length null-padded byte slice to a string,
// trimming everything from the first null byte onward.
func nullPaddedString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
