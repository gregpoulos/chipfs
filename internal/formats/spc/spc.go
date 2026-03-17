// Package spc parses SPC (SNES SPC700 Sound Format) files.
//
// Unlike NSF and GBS, each SPC file contains exactly one track. Collections are
// distributed as RSN archives (renamed RAR files containing multiple SPCs).
// The ID666 tag block embedded in the header provides rich per-track metadata
// including play duration, making SPC the easiest format to handle for duration
// estimation.
package spc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidMagic is returned when the data does not begin with the SPC magic string.
var ErrInvalidMagic = errors.New("not a valid SPC file: invalid magic bytes")

// spcMagic is the 33-byte ASCII string that begins every valid SPC file.
const spcMagic = "SNES-SPC700 Sound File Data v0.30"

// Header contains the parsed ID666 tag metadata from an SPC file.
type Header struct {
	SongTitle      string
	GameTitle      string
	Artist         string
	Dumper         string
	Comments       string
	PlayDurationMs int // converted from ID666 play_time (stored as integer seconds)
	FadeDurationMs int // converted from ID666 fade_length (stored as integer milliseconds)
}

// Parse parses an SPC file from raw bytes and returns its ID666 tag metadata.
func Parse(data []byte) (*Header, error) {
	if len(data) < 33 || !bytes.Equal(data[0:33], []byte(spcMagic)) {
		return nil, ErrInvalidMagic
	}
	if len(data) < 0xD2 {
		return nil, fmt.Errorf("spc: file truncated: need %d bytes for ID666 tags, got %d", 0xD2, len(data))
	}

	h := &Header{
		SongTitle: nullPaddedString(data[0x2E:0x4E]),
		GameTitle: nullPaddedString(data[0x4E:0x6E]),
		Dumper:    nullPaddedString(data[0x6E:0x7E]),
		Comments:  nullPaddedString(data[0x7E:0x9E]),
	}

	if isBinaryFormat(data) {
		// Binary format: durations are raw little-endian integers; artist is at 0xB0.
		playSec := uint32(data[0xA9]) | uint32(data[0xAA])<<8 | uint32(data[0xAB])<<16
		h.PlayDurationMs = int(playSec) * 1000
		h.FadeDurationMs = int(binary.LittleEndian.Uint32(data[0xAC:0xB0]))
		h.Artist = nullPaddedString(data[0xB0:0xD0])
	} else {
		// Text format: durations are ASCII decimal strings; artist is at 0xB1.
		h.PlayDurationMs = parseASCIIInt(data[0xA9:0xAC]) * 1000
		h.FadeDurationMs = parseASCIIInt(data[0xAC:0xB1])
		h.Artist = nullPaddedString(data[0xB1:0xD1])
	}

	return h, nil
}

// isBinaryFormat detects whether the ID666 tag uses binary or text encoding.
//
// The spec provides no definitive indicator. Two heuristics are applied in order:
//  1. If data[0xA0] == '/', the date field is "MM/DD/YYYY" → text format.
//  2. If data[0xA9] is an ASCII digit (0x30–0x39), infer text format.
//
// Edge case: binary files with 48–57 second durations will false-positive on
// heuristic 2. This is an accepted limitation of the spec.
func isBinaryFormat(data []byte) bool {
	if data[0xA0] == '/' {
		return false // date slash confirms text format
	}
	if data[0xA9] >= 0x30 && data[0xA9] <= 0x39 {
		return false // ASCII digit at duration field → text format
	}
	return true
}

// nullPaddedString converts a fixed-length null-padded byte slice to a string,
// trimming everything from the first null byte onward.
func nullPaddedString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// parseASCIIInt parses a decimal integer from a fixed-width ASCII byte slice,
// ignoring leading/trailing spaces and null bytes. Returns 0 if the field is
// blank or unparseable.
func parseASCIIInt(b []byte) int {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
