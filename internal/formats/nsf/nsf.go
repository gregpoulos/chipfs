// Package nsf parses NSF (NES Sound Format) and NSFe (Extended NES Sound Format) files.
//
// NSF is a compact format that stores the original NES music code extracted from
// a cartridge. A single file contains the complete soundtrack for a game. The
// 128-byte header provides global metadata (title, artist, copyright) and a track
// count; per-track titles and durations are only available in the NSFe extension.
package nsf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalidMagic is returned when the data does not begin with the NSF or NSFe magic bytes.
var ErrInvalidMagic = errors.New("not a valid NSF file: invalid magic bytes")

// magic is the five-byte sequence that begins every NSF file: "NESM" + 0x1A.
var magic = []byte{0x4E, 0x45, 0x53, 0x4D, 0x1A}

// nsfeMagic is the four-byte sequence that begins every NSFe file: "NSFE".
var nsfeMagic = []byte{0x4E, 0x53, 0x46, 0x45}

// Header contains the parsed metadata from an NSF or NSFe file.
type Header struct {
	Title      string
	Artist     string
	Copyright  string
	TrackCount int
	FirstTrack int         // 1-indexed default starting track
	Tracks     []TrackInfo // populated only for NSFe files with tlbl/time/fade chunks; nil otherwise
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
	if len(data) < 4 {
		return nil, ErrInvalidMagic
	}
	if bytes.Equal(data[0:4], nsfeMagic) {
		return parseNSFe(data)
	}
	if len(data) < 5 || !bytes.Equal(data[0:5], magic) {
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

// parseNSFe parses the chunk-based NSFe format.
func parseNSFe(data []byte) (*Header, error) {
	h := &Header{}
	hasINFO := false
	pos := 4 // skip "NSFE" magic

	for pos+8 <= len(data) {
		size := int(binary.LittleEndian.Uint32(data[pos:]))
		id := string(data[pos+4 : pos+8])
		pos += 8

		if id == "NEND" {
			break
		}
		if pos+size > len(data) {
			return nil, fmt.Errorf("nsfe: chunk %q data truncated", id)
		}
		chunk := data[pos : pos+size]
		pos += size

		switch id {
		case "INFO":
			if size < 8 {
				return nil, fmt.Errorf("nsfe: INFO chunk too short: need at least 8 bytes, got %d", size)
			}
			if size >= 9 {
				h.TrackCount = int(chunk[8])
			}
			if size >= 10 {
				h.FirstTrack = int(chunk[9]) + 1 // 0-indexed in file → 1-indexed in Header
			} else {
				h.FirstTrack = 1
			}
			hasINFO = true

		case "auth":
			strs := splitNullTerminated(chunk, 4)
			if len(strs) > 0 {
				h.Title = strs[0]
			}
			if len(strs) > 1 {
				h.Artist = strs[1]
			}
			if len(strs) > 2 {
				h.Copyright = strs[2]
			}

		case "tlbl":
			titles := splitNullTerminated(chunk, h.TrackCount)
			for i, t := range titles {
				ensureTracks(h, i+1)
				h.Tracks[i].Title = t
			}

		case "time":
			for i := 0; i+4 <= len(chunk); i += 4 {
				idx := i / 4
				if h.TrackCount > 0 && idx >= h.TrackCount {
					break
				}
				ensureTracks(h, idx+1)
				h.Tracks[idx].DurationMs = int(int32(binary.LittleEndian.Uint32(chunk[i:])))
			}

		case "fade":
			for i := 0; i+4 <= len(chunk); i += 4 {
				idx := i / 4
				if h.TrackCount > 0 && idx >= h.TrackCount {
					break
				}
				ensureTracks(h, idx+1)
				h.Tracks[idx].FadeMs = int(int32(binary.LittleEndian.Uint32(chunk[i:])))
			}
		}
	}

	if !hasINFO {
		return nil, fmt.Errorf("nsfe: required INFO chunk not found")
	}
	return h, nil
}

// ensureTracks grows h.Tracks to at least n entries if needed.
func ensureTracks(h *Header, n int) {
	if len(h.Tracks) < n {
		grown := make([]TrackInfo, n)
		copy(grown, h.Tracks)
		h.Tracks = grown
	}
}

// splitNullTerminated splits data on null bytes, returning up to max strings.
func splitNullTerminated(data []byte, max int) []string {
	var result []string
	for len(data) > 0 && (max <= 0 || len(result) < max) {
		i := bytes.IndexByte(data, 0)
		if i < 0 {
			result = append(result, string(data))
			break
		}
		result = append(result, string(data[:i]))
		data = data[i+1:]
	}
	return result
}

// nullPaddedString converts a fixed-length null-padded byte slice to a string,
// trimming everything from the first null byte onward.
func nullPaddedString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
