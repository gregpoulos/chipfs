package vfs_test

import (
	"testing"

	"github.com/gregpoulos/chipfs/internal/vfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRoot_RejectsEmptySourceDir(t *testing.T) {
	_, err := vfs.NewRoot("", vfs.Options{})
	assert.Error(t, err, "empty source directory must be rejected")
}

func TestNewRoot_RejectsNonExistentDir(t *testing.T) {
	_, err := vfs.NewRoot("/this/path/does/not/exist/chipfs-test", vfs.Options{})
	assert.Error(t, err, "non-existent source directory must be rejected")
}

func TestNewRoot_AcceptsValidDir(t *testing.T) {
	dir := t.TempDir()
	root, err := vfs.NewRoot(dir, vfs.Options{})
	require.NoError(t, err)
	assert.NotNil(t, root)
}

// TestMount_* tests require a mounted FUSE filesystem and are reserved for
// integration tests that run on Linux (or macOS with macFUSE installed).
// They live here as documentation of the expected behavior and are skipped
// in the standard unit test run.

func TestMount_VirtualDirAppearsNextToSourceFile(t *testing.T) {
	t.Skip("integration test: requires FUSE mount (Linux or macFUSE)")
}

func TestMount_TrackFilesAreEnumerated(t *testing.T) {
	t.Skip("integration test: requires FUSE mount (Linux or macFUSE)")
}

func TestMount_TrackFileReturnsValidWAV(t *testing.T) {
	t.Skip("integration test: requires FUSE mount (Linux or macFUSE)")
}
