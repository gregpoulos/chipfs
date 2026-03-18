// cmd/render is a developer tool for manual integration testing.
// It renders a single track from a chiptune file to a WAV file on disk,
// exercising the full pipeline (libgme → WAV muxer) without requiring a FUSE mount.
//
// Usage:
//
//	go run ./cmd/render -file <path> [-track <n>] [-out <path>] [-duration <ms>] [-fade <ms>]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregpoulos/chipfs/internal/gme"
	"github.com/gregpoulos/chipfs/internal/wav"
)

func main() {
	filePath := flag.String("file", "", "path to NSF, NSFe, GBS, or SPC file (required)")
	trackIdx := flag.Int("track", 0, "0-indexed track number (default: 0)")
	outPath := flag.String("out", "", "output WAV path (default: <stem>_track<N>.wav)")
	durationMs := flag.Int("duration", 0, "play duration in ms (0 = use file metadata)")
	fadeMs := flag.Int("fade", 0, "fade-out length in ms (0 = use file metadata or 8000)")
	flag.Parse()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "usage: render -file <path> [flags]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if err := run(*filePath, *trackIdx, *outPath, *durationMs, *fadeMs); err != nil {
		fmt.Fprintf(os.Stderr, "render: %v\n", err)
		os.Exit(1)
	}
}

func run(filePath string, trackIdx int, outPath string, overrideDurationMs, overrideFadeMs int) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	emu, err := gme.Open(data, 44100)
	if err != nil {
		return fmt.Errorf("opening with libgme: %w", err)
	}
	defer emu.Close()

	trackCount := emu.TrackCount()
	if trackIdx < 0 || trackIdx >= trackCount {
		return fmt.Errorf("track %d out of range (file has %d tracks, use 0–%d)",
			trackIdx, trackCount, trackCount-1)
	}

	ti, err := emu.TrackInfo(trackIdx)
	if err != nil {
		return fmt.Errorf("reading track info: %w", err)
	}

	// Resolve duration: flag → file metadata (ti.PlayMs) → 3-minute default.
	durationMs := overrideDurationMs
	if durationMs == 0 && ti.PlayMs > 0 {
		durationMs = ti.PlayMs
	}
	if durationMs == 0 {
		durationMs = 180_000
	}

	// Resolve fade length: flag → file metadata (ti.FadeMs) → 8-second default.
	fadeMs := overrideFadeMs
	if fadeMs == 0 && ti.FadeMs > 0 {
		fadeMs = ti.FadeMs
	}
	if fadeMs == 0 {
		fadeMs = 8_000
	}

	// Default output path: <stem>_track<N+1:02d>.wav in the working directory.
	if outPath == "" {
		stem := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		outPath = fmt.Sprintf("%s_track%02d.wav", stem, trackIdx+1)
	}

	// Render: loop Play() until TrackEnded, with a 15-minute safety cap.
	if err := emu.StartTrack(trackIdx); err != nil {
		return fmt.Errorf("starting track: %w", err)
	}
	emu.SetFade(durationMs, fadeMs)

	const (
		chunkLen  = 4096
		maxPlayMs = 20 * 60 * 1000 // must match vfs.maxPlayMs
		maxFadeMs = 60 * 1000      // must match vfs.maxFadeMs
	)
	maxSamples := ((maxPlayMs + maxFadeMs) * 44100 / 1000) * 2
	allSamples := make([]int16, 0, ((durationMs+fadeMs)*44100/1000)*2)
	chunk := make([]int16, chunkLen)

	for !emu.TrackEnded() && len(allSamples) < maxSamples {
		if err := emu.Play(chunk); err != nil {
			return fmt.Errorf("rendering: %w", err)
		}
		allSamples = append(allSamples, chunk...)
	}

	// Mux to WAV with ID3 metadata.
	title := ti.Title
	if title == "" {
		title = fmt.Sprintf("Track %d", trackIdx+1)
	}
	wavData, err := wav.Encode(allSamples, wav.Options{
		SampleRate: 44100,
		Channels:   2,
		Metadata: wav.Metadata{
			Title:  title,
			Artist: ti.Author,
			Album:  ti.Game,
			Track:  trackIdx + 1,
		},
	})
	if err != nil {
		return fmt.Errorf("encoding WAV: %w", err)
	}

	if err := os.WriteFile(outPath, wavData, 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Printf("Rendered: %s — %s — track %d/%d — %dms → %s (%.1f MB)\n",
		ti.Game, title, trackIdx+1, trackCount,
		durationMs, outPath, float64(len(wavData))/(1024*1024))

	return nil
}
