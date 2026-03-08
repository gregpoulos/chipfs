// Package nsf parses NSF (NES Sound Format) and NSFe (Extended NES Sound Format) files.
//
// NSF is a compact format that stores the original NES music code extracted from
// a cartridge. A single file contains the complete soundtrack for a game. The
// 128-byte header provides global metadata (title, artist, copyright) and a track
// count; per-track titles and durations are only available in the NSFe extension.
package nsf

import "errors"

// ErrInvalidMagic is returned when the data does not begin with the NSF or NSFe magic bytes.
var ErrInvalidMagic = errors.New("not a valid NSF file: invalid magic bytes")

// NSF magic: "NESM" + 0x1A
var magic = []byte{0x4E, 0x45, 0x53, 0x4D, 0x1A}

// Header contains the parsed metadata from an NSF or NSFe file.
type Header struct {
	Title      string
	Artist     string
	Copyright  string
	TrackCount int
	FirstTrack int    // 1-indexed default starting track
	Tracks     []TrackInfo // populated only for NSFe files; nil for plain NSF
}

// TrackInfo holds per-track metadata from NSFe extension chunks.
// Fields are zero-valued when the corresponding NSFe chunk is absent.
type TrackInfo struct {
	Title      string // from NSFe tlbl chunk
	DurationMs int    // from NSFe time chunk; 0 means use configured default
	FadeMs     int    // from NSFe fade chunk; 0 means use configured default
}

// Parse parses an NSF or NSFe file from raw bytes and returns its header metadata.
// It detects NSFe by the presence of the "NSFE" magic and parses extension chunks
// to populate per-track TrackInfo where available.
func Parse(data []byte) (*Header, error) {
	// TODO: implement
	// 1. Check magic bytes at [0:5]
	// 2. Read header fields using encoding/binary (little-endian)
	// 3. If version == 2 or magic == "NSFE", parse extension chunks
	return nil, errors.New("not implemented")
}
