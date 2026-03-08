// Package wav produces WAV-format audio files from raw int16 PCM samples and
// embeds track metadata as a RIFF id3 chunk so media servers like Navidrome
// can read Artist, Album, Title, and Track Number.
//
// WAV is chosen as the primary output format because its file size is
// mathematically exact given a known sample count, eliminating the need for
// estimates in FUSE getattr responses.
package wav

import "errors"

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

// Encode encodes the given stereo int16 PCM samples into a complete WAV byte
// slice. An ID3v2 tag is embedded as a RIFF "id3 " chunk before the "data"
// chunk so that taglib-based scanners (including Navidrome) can read the
// metadata without a separate sidecar file.
func Encode(samples []int16, opts Options) ([]byte, error) {
	// TODO: implement
	// 1. Build ID3v2 tag bytes (TIT2, TPE1, TALB, TRCK, TDRC, COMM frames)
	// 2. Write RIFF header: "RIFF", total_size, "WAVE"
	// 3. Write fmt chunk: PCM format, channels, sample rate, byte rate, block align, bit depth
	// 4. Write id3 chunk: ID3v2 tag bytes (RIFF chunk tag "id3 ", size, data)
	// 5. Write data chunk: "data", PCM byte count, raw int16 samples (little-endian)
	return nil, errors.New("not implemented")
}

// EstimatedSize returns the exact byte length that Encode will produce for a
// track of the given duration. This is used to populate the FUSE getattr file
// size before emulation begins, allowing media servers to allocate buffers
// correctly.
//
// The estimate is exact for WAV/PCM because sample count is deterministic:
//
//	samples = (durationMs / 1000) * sampleRate * channels
//	pcmBytes = samples * 2  (int16 = 2 bytes per sample)
//	total = riffHeaderSize + fmtChunkSize + id3ChunkSize + dataChunkSize + pcmBytes
func EstimatedSize(durationMs int, opts Options) int64 {
	// TODO: implement (must match Encode output exactly for a given durationMs)
	return 0
}
