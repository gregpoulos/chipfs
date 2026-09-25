// Package wav produces WAV-format audio files from raw int16 PCM samples and
// embeds track metadata as a RIFF id3 chunk so media servers like Navidrome
// can read Artist, Album, Title, and Track Number.
//
// WAV is chosen as the primary output format because its file size is
// mathematically exact given a known sample count, eliminating the need for
// estimates in FUSE getattr responses.
package wav

import (
	"encoding/binary"
	"slices"
	"strconv"
)

// Metadata holds the tag information to embed in the WAV file.
type Metadata struct {
	Title  string
	Artist string
	Album  string
	// AlbumArtist (ID3 TPE2) groups tracks into one album when their per-track
	// artists differ. Omitted from the tag when empty.
	AlbumArtist string
	Track       int
}

// Options configures WAV encoding parameters.
type Options struct {
	SampleRate int
	Channels   int
	Metadata   Metadata
}

// Encode encodes the given int16 PCM samples into a complete WAV byte slice.
// An ID3v2.4 tag is embedded as a RIFF "id3 " chunk and a RIFF "LIST INFO"
// chunk before the "data" chunk, providing metadata to both taglib-based
// scanners (including Navidrome) and older WAV parsers that only read INFO.
//
// Output layout:
//
//	RIFF header (12 bytes) → fmt chunk (24 bytes) → id3 chunk → LIST INFO chunk → data chunk
func Encode(samples []int16, opts Options) []byte {
	pcmBytes := len(samples) * 2
	b := header(pcmBytes, opts)
	n := len(b)
	b = slices.Grow(b, pcmBytes)[:n+pcmBytes]
	for i, s := range samples {
		binary.LittleEndian.PutUint16(b[n+2*i:], uint16(s))
	}
	return b
}

// HeaderBytes returns the prefix of Encode's output for a track of the given
// duration: everything up to and including the "data" chunk header, with no
// PCM samples. It can be served before the track is rendered.
func HeaderBytes(durationMs int, opts Options) []byte {
	return header(SampleCount(durationMs, opts)*2, opts)
}

// EstimatedSize returns the exact byte length Encode produces for a track of
// the given duration, so it can be reported as the file size before rendering.
func EstimatedSize(durationMs int, opts Options) int64 {
	return int64(len(HeaderBytes(durationMs, opts)) + SampleCount(durationMs, opts)*2)
}

// SampleCount returns the number of interleaved int16 samples in a track of
// the given duration.
func SampleCount(durationMs int, opts Options) int {
	return (durationMs * opts.SampleRate / 1000) * opts.Channels
}

// header builds every byte of the WAV file before the PCM data, for a data
// chunk of pcmBytes.
func header(pcmBytes int, opts Options) []byte {
	le := binary.LittleEndian
	id3 := buildID3v2(opts.Metadata)

	b := append([]byte("RIFF"), 0, 0, 0, 0) // size patched below
	b = append(b, "WAVE"...)

	b = append(b, "fmt "...)
	b = le.AppendUint32(b, 16) // always 16-byte PCM
	b = le.AppendUint16(b, 1)  // PCM
	b = le.AppendUint16(b, uint16(opts.Channels))
	b = le.AppendUint32(b, uint32(opts.SampleRate))
	b = le.AppendUint32(b, uint32(opts.SampleRate*opts.Channels*2)) // byte rate
	b = le.AppendUint16(b, uint16(opts.Channels*2))                 // block align
	b = le.AppendUint16(b, 16)                                      // bits per sample

	b = append(b, "id3 "...)
	b = le.AppendUint32(b, uint32(len(id3)))
	b = append(b, id3...)
	if len(id3)%2 == 1 {
		b = append(b, 0) // RIFF pad byte
	}

	b = append(b, buildListInfo(opts.Metadata)...) // omitted when metadata is empty

	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(pcmBytes))

	le.PutUint32(b[4:], uint32(len(b)-8+pcmBytes))
	return b
}

// buildListInfo constructs a RIFF LIST INFO chunk containing INAM (title),
// IART (artist), and IPRD (album/product) subchunks. Returns nil when no
// metadata fields are set, so the chunk is omitted entirely for bare WAV files.
//
// LIST INFO is a standard RIFF extension understood by Windows Media Player,
// Winamp, and other WAV parsers that do not read the "id3 " chunk. Both chunks
// coexist in the same file; taglib-based parsers (Navidrome) prefer the id3 chunk.
func buildListInfo(meta Metadata) []byte {
	var subchunks []byte
	if meta.Title != "" {
		subchunks = append(subchunks, infoSubchunk("INAM", meta.Title)...)
	}
	if meta.Artist != "" {
		subchunks = append(subchunks, infoSubchunk("IART", meta.Artist)...)
	}
	if meta.Album != "" {
		subchunks = append(subchunks, infoSubchunk("IPRD", meta.Album)...)
	}
	if len(subchunks) == 0 {
		return nil
	}
	// LIST chunk: "LIST" (4) + size (4) + "INFO" (4) + subchunks
	buf := make([]byte, 12+len(subchunks))
	copy(buf[0:], "LIST")
	binary.LittleEndian.PutUint32(buf[4:], uint32(4+len(subchunks)))
	copy(buf[8:], "INFO")
	copy(buf[12:], subchunks)
	return buf
}

// infoSubchunk builds a single LIST INFO subchunk: 4-byte ID + 4-byte LE size
// + null-terminated string, padded to even length.
func infoSubchunk(id, text string) []byte {
	strLen := len(text) + 1 // include null terminator
	total := 8 + paddedSize(strLen)
	buf := make([]byte, total)
	copy(buf[0:], id)
	binary.LittleEndian.PutUint32(buf[4:], uint32(strLen))
	copy(buf[8:], text)
	// buf[8+len(text)] = 0x00 (null terminator, already zero from make)
	return buf
}

// buildID3v2 constructs an ID3v2.4 tag from the given metadata. It is v2.4
// rather than v2.3 because the frames are UTF-8, which v2.3 does not define.
// An empty-metadata call still returns the 10-byte ID3v2 header (no frames).
func buildID3v2(meta Metadata) []byte {
	var frames []byte
	frames = append(frames, textFrame("TIT2", meta.Title)...)
	frames = append(frames, textFrame("TPE1", meta.Artist)...)
	frames = append(frames, textFrame("TALB", meta.Album)...)
	frames = append(frames, textFrame("TPE2", meta.AlbumArtist)...)
	if meta.Track > 0 {
		frames = append(frames, textFrame("TRCK", strconv.Itoa(meta.Track))...)
	}

	// ID3v2.4 header: "ID3" + version (0x04 0x00) + flags + syncsafe size
	tag := make([]byte, 10, 10+len(frames))
	copy(tag, "ID3")
	tag[3] = 0x04 // ID3v2.4
	tag[4] = 0x00 // revision
	tag[5] = 0x00 // no flags
	syncsafe(tag[6:10], len(frames))
	return append(tag, frames...)
}

// textFrame builds an ID3v2.4 text frame (TIT2, TPE1, TALB, TPE2, TRCK).
// Returns nil if text is empty.
func textFrame(id, text string) []byte {
	if text == "" {
		return nil
	}
	// data = encoding byte (0x03 = UTF-8) + text
	dataLen := 1 + len(text)
	frame := make([]byte, 11+len(text))
	copy(frame[0:4], id)
	syncsafe(frame[4:8], dataLen)
	// frame[8], frame[9] = 0x00, 0x00 (flags, already zero)
	frame[10] = 0x03 // UTF-8 encoding
	copy(frame[11:], text)
	return frame
}

// syncsafe encodes n as a 4-byte ID3v2 syncsafe integer into dst.
// Each byte uses only the low 7 bits; the high bit is always 0.
func syncsafe(dst []byte, n int) {
	dst[0] = byte((n >> 21) & 0x7F)
	dst[1] = byte((n >> 14) & 0x7F)
	dst[2] = byte((n >> 7) & 0x7F)
	dst[3] = byte(n & 0x7F)
}

// paddedSize returns the size of a RIFF chunk's in-file footprint (data + optional
// 1-byte pad to maintain even alignment). The pad byte is not counted in the
// chunk's size field but does occupy space in the file.
func paddedSize(n int) int {
	if n%2 == 0 {
		return n
	}
	return n + 1
}
