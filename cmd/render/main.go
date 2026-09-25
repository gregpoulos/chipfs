// cmd/render is a developer tool for manual integration testing. It renders a
// single track to a WAV file on disk exactly as the mount would serve it,
// without requiring a FUSE mount.
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

	"github.com/gregpoulos/chipfs/internal/vfs"
)

func main() {
	filePath := flag.String("file", "", "path to NSF, NSFe, GBS, or SPC file (required)")
	trackIdx := flag.Int("track", 0, "0-indexed track number")
	outPath := flag.String("out", "", "output WAV path (default: <stem>_track<N>.wav)")
	durationMs := flag.Int("duration", 0, "play duration in ms (0 = what the mount would use)")
	fadeMs := flag.Int("fade", 0, "fade-out length in ms (0 = what the mount would use)")
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

func run(filePath string, trackIdx int, outPath string, durationMs, fadeMs int) error {
	t, err := vfs.RenderTrack(filePath, trackIdx, durationMs, fadeMs, vfs.Options{})
	if err != nil {
		return err
	}
	if outPath == "" {
		stem := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		outPath = fmt.Sprintf("%s_track%02d.wav", stem, trackIdx+1)
	}
	if err := os.WriteFile(outPath, t.WAV, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	fmt.Printf("Rendered: %s — %s — track %d/%d — %dms + %dms fade → %s (%.1f MB)\n",
		t.Metadata.Album, t.Metadata.Title, trackIdx+1, t.TrackCount,
		t.PlayMs, t.FadeMs, outPath, float64(len(t.WAV))/(1024*1024))
	return nil
}
