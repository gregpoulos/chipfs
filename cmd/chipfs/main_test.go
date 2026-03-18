package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArgs_RequiredFlags(t *testing.T) {
	_, err := parseArgs([]string{})
	assert.Error(t, err, "missing -source and -mountpoint must error")

	_, err = parseArgs([]string{"-source", "/tmp/chips"})
	assert.Error(t, err, "missing -mountpoint must error")

	_, err = parseArgs([]string{"-mountpoint", "/mnt/chipfs"})
	assert.Error(t, err, "missing -source must error")
}

func TestParseArgs_Defaults(t *testing.T) {
	cfg, err := parseArgs([]string{"-source", "/tmp/chips", "-mountpoint", "/mnt/chipfs"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/chips", cfg.source)
	assert.Equal(t, "/mnt/chipfs", cfg.mountpoint)
	assert.False(t, cfg.allowOther, "-allow_other must default to false")
	assert.Equal(t, 180, cfg.defaultLengthSec, "-default_length must default to 180")
	assert.Equal(t, 8, cfg.fadeLengthSec, "-fade_length must default to 8")
	assert.Equal(t, 256, cfg.cacheSizeMb, "-cache_size_mb must default to 256")
}

func TestParseArgs_AllowOther(t *testing.T) {
	cfg, err := parseArgs([]string{"-source", "/tmp/chips", "-mountpoint", "/mnt/chipfs", "-allow_other"})
	require.NoError(t, err)
	assert.True(t, cfg.allowOther, "-allow_other flag must set allowOther=true")
}

func TestParseArgs_MountOptions(t *testing.T) {
	cfg, err := parseArgs([]string{
		"-source", "/tmp/chips",
		"-mountpoint", "/mnt/chipfs",
		"-default_length", "120",
		"-fade_length", "5",
		"-cache_size_mb", "128",
	})
	require.NoError(t, err)
	assert.Equal(t, 120, cfg.defaultLengthSec)
	assert.Equal(t, 5, cfg.fadeLengthSec)
	assert.Equal(t, 128, cfg.cacheSizeMb)
}
