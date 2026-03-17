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

	return &Header{
		SongTitle:      nullPaddedString(data[0x2E:0x4E]),
		GameTitle:      nullPaddedString(data[0x4E:0x6E]),
		Dumper:         nullPaddedString(data[0x6E:0x7E]),
		Comments:       nullPaddedString(data[0x7E:0x9E]),
		Artist:         nullPaddedString(data[0xB1:0xD1]),
		PlayDurationMs: parseASCIIInt(data[0xA9:0xAC]) * 1000,
		FadeDurationMs: parseASCIIInt(data[0xAC:0xB1]),
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

// parseASCIIInt parses a decimal integer from a fixed-width ASCII byte slice,
// ignoring leading/trailing spaces. Returns 0 if the field is blank or unparseable.
func parseASCIIInt(b []byte) int {
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
