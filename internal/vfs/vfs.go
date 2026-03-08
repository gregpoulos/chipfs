// Package vfs implements the FUSE filesystem nodes for ChipFS using hanwen/go-fuse.
//
// The package is intentionally thin: it translates FUSE kernel requests into
// calls to the format parsers, gme emulator, and track cache in other internal
// packages. All business logic lives in those packages; vfs only handles the
// mechanics of presenting virtual files and directories to the OS.
//
// # Virtual Directory Structure
//
// For each chiptune file foo.nsf in the source directory, ChipFS presents:
//
//	foo.nsf           (passthrough read of the real file)
//	foo/              (virtual directory, one entry per track)
//	  01 - Title.wav  (virtual WAV file, rendered on demand)
//	  02 - Title.wav
//	  ...
//
// # Node Types
//
//   - Root:      top-level node; lists source dir contents + virtual siblings
//   - ChipDir:   virtual directory for one chiptune file; handles Lookup + Readdir
//   - TrackFile: virtual WAV file for one track; handles Getattr + Read
package vfs

import (
	"errors"

	"github.com/hanwen/go-fuse/v2/fs"
)

// Root is the top-level FUSE node that presents the contents of the source
// directory augmented with virtual sibling directories for each chiptune file.
type Root struct {
	fs.Inode
	sourceDir string
}

// NewRoot creates a Root node backed by the given source directory path.
// Returns an error if sourceDir is empty.
func NewRoot(sourceDir string) (*Root, error) {
	if sourceDir == "" {
		return nil, errors.New("vfs: source directory must not be empty")
	}
	// TODO: verify sourceDir exists and is readable
	// TODO: initialize cache and scan source directory
	return &Root{sourceDir: sourceDir}, nil
}

// ChipDir is a virtual directory representing the tracks of one chiptune file.
type ChipDir struct {
	fs.Inode
	// TODO: add fields: sourcePath string, header interface{}, cache *cache.Cache
}

// TrackFile is a virtual WAV file representing one track of a chiptune file.
type TrackFile struct {
	fs.Inode
	// TODO: add fields: sourcePath string, trackIndex int, estimatedSize int64, cache *cache.Cache
}
