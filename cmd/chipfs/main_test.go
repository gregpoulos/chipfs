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
}

func TestParseArgs_AllowOther(t *testing.T) {
	cfg, err := parseArgs([]string{"-source", "/tmp/chips", "-mountpoint", "/mnt/chipfs", "-allow_other"})
	require.NoError(t, err)
	assert.True(t, cfg.allowOther, "-allow_other flag must set allowOther=true")
}
