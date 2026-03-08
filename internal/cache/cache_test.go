package cache_test

import (
	"testing"

	"github.com/gregpoulos/chipfs/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_MissReturnsNilFalse(t *testing.T) {
	c := cache.New(64 * 1024 * 1024)
	data, ok := c.Get("game.nsf", 0)
	assert.False(t, ok)
	assert.Nil(t, data)
}

func TestCache_SetThenGet(t *testing.T) {
	c := cache.New(64 * 1024 * 1024)
	payload := []byte("wav file bytes here")

	c.Set("game.nsf", 2, payload)

	data, ok := c.Get("game.nsf", 2)
	require.True(t, ok)
	assert.Equal(t, payload, data)
}

func TestCache_DifferentTracksAreIndependent(t *testing.T) {
	c := cache.New(64 * 1024 * 1024)
	c.Set("game.nsf", 0, []byte("track 0 data"))
	c.Set("game.nsf", 1, []byte("track 1 data"))

	d0, ok0 := c.Get("game.nsf", 0)
	d1, ok1 := c.Get("game.nsf", 1)

	require.True(t, ok0)
	require.True(t, ok1)
	assert.Equal(t, []byte("track 0 data"), d0)
	assert.Equal(t, []byte("track 1 data"), d1)
}

func TestCache_DifferentSourceFilesAreIndependent(t *testing.T) {
	c := cache.New(64 * 1024 * 1024)
	c.Set("game_a.nsf", 0, []byte("game a, track 0"))
	c.Set("game_b.nsf", 0, []byte("game b, track 0"))

	a, okA := c.Get("game_a.nsf", 0)
	b, okB := c.Get("game_b.nsf", 0)

	require.True(t, okA)
	require.True(t, okB)
	assert.Equal(t, []byte("game a, track 0"), a)
	assert.Equal(t, []byte("game b, track 0"), b)
}

func TestCache_EvictsWhenOverCapacity(t *testing.T) {
	// Cache capacity is 10 bytes; two 6-byte entries cannot both fit.
	// After adding entry B, entry A (the LRU) must be evicted.
	c := cache.New(10)
	c.Set("game.nsf", 0, []byte("aaaaaa")) // 6 bytes
	c.Set("game.nsf", 1, []byte("bbbbbb")) // 6 bytes — triggers eviction of track 0

	_, okA := c.Get("game.nsf", 0)
	_, okB := c.Get("game.nsf", 1)

	assert.False(t, okA, "track 0 should have been evicted")
	assert.True(t, okB, "track 1 should remain in cache")
}
