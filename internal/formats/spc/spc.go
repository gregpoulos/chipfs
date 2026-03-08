// Package spc parses SPC (SNES SPC700 Sound Format) files.
//
// Unlike NSF and GBS, each SPC file contains exactly one track. Collections are
// distributed as RSN archives (renamed RAR files containing multiple SPCs).
// The ID666 tag block embedded in the header provides rich per-track metadata
// including play duration, making SPC the easiest format to handle for duration
// estimation.
package spc

import "errors"

// ErrInvalidMagic is returned when the data does not begin with the SPC magic string.
var ErrInvalidMagic = errors.New("not a valid SPC file: invalid magic bytes")

// spcMagic is the 33-byte ASCII string at the start of every valid SPC file.
const spcMagic = "SNES-SPC700 Sound File Data v0.30"

// Header contains the parsed ID666 tag metadata from an SPC file.
type Header struct {
	SongTitle      string
	GameTitle      string
	Artist         string
	Dumper         string
	Comments       string
	PlayDurationMs int // converted from the ID666 play_time field (stored as seconds)
	FadeDurationMs int // converted from the ID666 fade_length field (stored as milliseconds)
}

// Parse parses an SPC file from raw bytes and returns its ID666 tag metadata.
func Parse(data []byte) (*Header, error) {
	// TODO: implement
	// 1. Verify 33-byte magic at [0:33]
	// 2. Check ID666 tag present flag at [0x23]
	// 3. Read text-format ID666 fields at fixed offsets
	//    song [0x2E:0x4E], game [0x4E:0x6E], dumper [0x6E:0x7E]
	//    comments [0x7E:0x9E], artist [0xB1:0xD1]
	// 4. Parse play duration (ASCII decimal seconds) at [0xA9:0xAC], multiply by 1000
	// 5. Parse fade length (ASCII decimal ms) at [0xAC:0xB1]
	return nil, errors.New("not implemented")
}
