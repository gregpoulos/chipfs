package cache_test

import (
	"fmt"
	"sync"
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

func TestCache_GetPromotesToMRU(t *testing.T) {
	// Add A then B (A is LRU). Access A to promote it. Then add C, which should
	// evict B (now the LRU), not A.
	c := cache.New(15)
	c.Set("game.nsf", 0, []byte("aaaaaa")) // 6 bytes
	c.Set("game.nsf", 1, []byte("bbbbbb")) // 6 bytes — total 12, A is LRU

	_, ok := c.Get("game.nsf", 0) // promote A to MRU; B is now LRU
	require.True(t, ok)

	c.Set("game.nsf", 2, []byte("cccccc")) // 6 bytes — exceeds 15, evicts B

	_, okA := c.Get("game.nsf", 0)
	_, okB := c.Get("game.nsf", 1)
	_, okC := c.Get("game.nsf", 2)

	assert.True(t, okA, "A was promoted and should survive")
	assert.False(t, okB, "B was LRU and should be evicted")
	assert.True(t, okC, "C was just added and should be present")
}

func TestCache_OverwriteUpdatesSize(t *testing.T) {
	// Overwriting an entry with larger data must not leak the old bytes into
	// the used-byte accounting.
	c := cache.New(20)
	c.Set("game.nsf", 0, []byte("small"))      // 5 bytes
	c.Set("game.nsf", 0, []byte("much bigger")) // 11 bytes — replaces the 5-byte entry

	data, ok := c.Get("game.nsf", 0)
	require.True(t, ok)
	assert.Equal(t, []byte("much bigger"), data)

	// A second entry of 10 bytes should still fit (11 + 10 = 21 > 20, so it
	// would evict the first entry but not fail silently).
	c.Set("game.nsf", 1, []byte("0123456789")) // 10 bytes — evicts track 0
	_, ok0 := c.Get("game.nsf", 0)
	_, ok1 := c.Get("game.nsf", 1)
	assert.False(t, ok0, "track 0 should be evicted")
	assert.True(t, ok1)
}

func TestCache_EntryLargerThanCapacity(t *testing.T) {
	// An entry larger than maxBytes should still be stored (not silently dropped).
	// The cache exceeds capacity temporarily; the entry is the only thing kept.
	c := cache.New(4)
	c.Set("game.nsf", 0, []byte("hello")) // 5 bytes > capacity of 4

	data, ok := c.Get("game.nsf", 0)
	require.True(t, ok, "oversized entry must not be silently dropped")
	assert.Equal(t, []byte("hello"), data)
}

func TestCache_ConcurrentAccess(t *testing.T) {
	// Hammer the cache from multiple goroutines to surface data races.
	// Run with: go test -race ./internal/cache/...
	c := cache.New(1024 * 1024)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("game%d.nsf", i%5)
			c.Set(key, i, make([]byte, 1024))
			c.Get(key, i)
		}(i)
	}
	wg.Wait()
}
