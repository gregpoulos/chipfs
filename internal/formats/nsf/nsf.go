// Package nsf parses NSF (NES Sound Format) and NSFe (Extended NES Sound Format) files.
//
// NSF is a compact format that stores the original NES music code extracted from
// a cartridge. A single file contains the complete soundtrack for a game. The
// 128-byte header provides global metadata (title, artist, copyright) and a track
// count; per-track titles and durations are only available in the NSFe extension.
package nsf

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrInvalidMagic is returned when the data does not begin with the NSF or NSFe magic bytes.
var ErrInvalidMagic = errors.New("not a valid NSF file: invalid magic bytes")

// magic is the five-byte sequence that begins every NSF file: "NESM" + 0x1A.
var magic = []byte{0x4E, 0x45, 0x53, 0x4D, 0x1A}

// Header contains the parsed metadata from an NSF or NSFe file.
type Header struct {
	Title      string
	Artist     string
	Copyright  string
	TrackCount int
	FirstTrack int     // 1-indexed default starting track
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
func Parse(data []byte) (*Header, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("nsf: data too short to contain magic bytes")
	}
	if !bytes.Equal(data[0:5], magic) {
		return nil, ErrInvalidMagic
	}
	if len(data) < 128 {
		return nil, fmt.Errorf("nsf: header truncated: need 128 bytes, got %d", len(data))
	}

	return &Header{
		TrackCount: int(data[6]),
		FirstTrack: int(data[7]),
		Title:      nullPaddedString(data[14:46]),
		Artist:     nullPaddedString(data[46:78]),
		Copyright:  nullPaddedString(data[78:110]),
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
