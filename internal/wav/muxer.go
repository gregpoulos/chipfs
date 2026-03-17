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
	"strconv"
)

// Metadata holds the tag information to embed in the WAV file.
type Metadata struct {
	Title   string
	Artist  string
	Album   string
	Track   int
	Year    string
	Comment string
}

// Options configures WAV encoding parameters.
type Options struct {
	SampleRate int
	Channels   int
	Metadata   Metadata
}

// Encode encodes the given int16 PCM samples into a complete WAV byte slice.
// An ID3v2.3 tag is embedded as a RIFF "id3 " chunk before the "data" chunk
// so that taglib-based scanners (including Navidrome) can read the metadata
// without a separate sidecar file.
//
// Output layout:
//
//	RIFF header (12 bytes) → fmt chunk (24 bytes) → id3 chunk → data chunk
func Encode(samples []int16, opts Options) ([]byte, error) {
	id3Data := buildID3v2(opts.Metadata)
	id3PaddedSize := paddedSize(len(id3Data))
	pcmBytes := len(samples) * 2

	totalSize := 12 + 24 + 8 + id3PaddedSize + 8 + pcmBytes
	buf := make([]byte, totalSize)
	pos := 0

	// RIFF header
	pos += copy(buf[pos:], "RIFF")
	binary.LittleEndian.PutUint32(buf[pos:], uint32(totalSize-8))
	pos += 4
	pos += copy(buf[pos:], "WAVE")

	// fmt chunk (always 16-byte PCM)
	pos += copy(buf[pos:], "fmt ")
	binary.LittleEndian.PutUint32(buf[pos:], 16)
	pos += 4
	binary.LittleEndian.PutUint16(buf[pos:], 1) // PCM
	pos += 2
	binary.LittleEndian.PutUint16(buf[pos:], uint16(opts.Channels))
	pos += 2
	binary.LittleEndian.PutUint32(buf[pos:], uint32(opts.SampleRate))
	pos += 4
	binary.LittleEndian.PutUint32(buf[pos:], uint32(opts.SampleRate*opts.Channels*2)) // byte rate
	pos += 4
	binary.LittleEndian.PutUint16(buf[pos:], uint16(opts.Channels*2)) // block align
	pos += 2
	binary.LittleEndian.PutUint16(buf[pos:], 16) // bits per sample
	pos += 2

	// id3 chunk
	pos += copy(buf[pos:], "id3 ")
	binary.LittleEndian.PutUint32(buf[pos:], uint32(len(id3Data)))
	pos += 4
	pos += copy(buf[pos:], id3Data)
	pos += id3PaddedSize - len(id3Data) // zero pad byte if needed

	// data chunk
	pos += copy(buf[pos:], "data")
	binary.LittleEndian.PutUint32(buf[pos:], uint32(pcmBytes))
	pos += 4
	for _, s := range samples {
		binary.LittleEndian.PutUint16(buf[pos:], uint16(s))
		pos += 2
	}

	return buf, nil
}

// EstimatedSize returns the exact byte length that Encode will produce for a
// track of the given duration. This is used to populate the FUSE getattr file
// size before emulation begins, allowing media servers to allocate buffers
// correctly.
//
// The estimate is exact because WAV/PCM file size is fully determined by
// duration, sample rate, channel count, and the fixed-size ID3v2 tag.
func EstimatedSize(durationMs int, opts Options) int64 {
	id3PaddedSize := paddedSize(len(buildID3v2(opts.Metadata)))
	pcmBytes := (durationMs * opts.SampleRate / 1000) * opts.Channels * 2
	return int64(12 + 24 + 8 + id3PaddedSize + 8 + pcmBytes)
}

// buildID3v2 constructs an ID3v2.3 tag from the given metadata.
// An empty-metadata call still returns the 10-byte ID3v2 header (no frames).
func buildID3v2(meta Metadata) []byte {
	var frames []byte
	frames = append(frames, textFrame("TIT2", meta.Title)...)
	frames = append(frames, textFrame("TPE1", meta.Artist)...)
	frames = append(frames, textFrame("TALB", meta.Album)...)
	if meta.Track > 0 {
		frames = append(frames, textFrame("TRCK", strconv.Itoa(meta.Track))...)
	}
	if meta.Year != "" {
		frames = append(frames, textFrame("TYER", meta.Year)...)
	}
	if meta.Comment != "" {
		frames = append(frames, commentFrame(meta.Comment)...)
	}

	// ID3v2.3 header: "ID3" + version (0x03 0x00) + flags + syncsafe size
	tag := make([]byte, 10, 10+len(frames))
	copy(tag, "ID3")
	tag[3] = 0x03 // ID3v2.3
	tag[4] = 0x00 // revision
	tag[5] = 0x00 // no flags
	syncsafe(tag[6:10], len(frames))
	return append(tag, frames...)
}

// textFrame builds an ID3v2.3 text frame (TIT2, TPE1, TALB, TRCK, TYER, etc.).
// Returns nil if text is empty.
func textFrame(id, text string) []byte {
	if text == "" {
		return nil
	}
	// data = encoding byte (0x03 = UTF-8) + text
	dataLen := 1 + len(text)
	frame := make([]byte, 11+len(text))
	copy(frame[0:4], id)
	frame[4] = byte(dataLen >> 24)
	frame[5] = byte(dataLen >> 16)
	frame[6] = byte(dataLen >> 8)
	frame[7] = byte(dataLen)
	// frame[8], frame[9] = 0x00, 0x00 (flags, already zero)
	frame[10] = 0x03 // UTF-8 encoding
	copy(frame[11:], text)
	return frame
}

// commentFrame builds an ID3v2.3 COMM frame.
func commentFrame(comment string) []byte {
	if comment == "" {
		return nil
	}
	// data = encoding (1) + language (3) + short desc null (1) + text
	data := make([]byte, 5+len(comment))
	data[0] = 0x03          // UTF-8
	copy(data[1:4], "eng")  // language
	data[4] = 0x00          // empty short description, null-terminated
	copy(data[5:], comment)

	frame := make([]byte, 10+len(data)) // header (10) + data
	copy(frame[0:4], "COMM")
	frame[4] = byte(len(data) >> 24)
	frame[5] = byte(len(data) >> 16)
	frame[6] = byte(len(data) >> 8)
	frame[7] = byte(len(data))
	// frame[8], frame[9] = 0x00, 0x00 (flags, already zero)
	copy(frame[10:], data)
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
