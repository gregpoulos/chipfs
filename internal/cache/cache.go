// Package cache provides an LRU in-memory store for fully-rendered WAV track data.
//
// When a track is first requested, the emulator renders it entirely into memory.
// Subsequent reads—including backward seeks—are served from this cache instantly,
// avoiding re-emulation. Entries are evicted in least-recently-used order when
// the total cached size exceeds the configured capacity.
package cache

// Cache is a thread-safe LRU store keyed by (source file path, track index).
type Cache struct {
	// TODO: implement using container/list + map for O(1) LRU
	maxBytes int64
}

// New creates a Cache with the given maximum byte capacity.
func New(maxBytes int64) *Cache {
	// TODO: implement
	return &Cache{maxBytes: maxBytes}
}

// Get retrieves the cached WAV data for a given source file path and track index.
// Returns (nil, false) on a cache miss.
func (c *Cache) Get(sourcePath string, trackIndex int) ([]byte, bool) {
	// TODO: implement
	return nil, false
}

// Set stores WAV data for a given source file path and track index.
// If adding the entry would exceed the cache capacity, least-recently-used
// entries are evicted until capacity is satisfied.
func (c *Cache) Set(sourcePath string, trackIndex int, data []byte) {
	// TODO: implement
}
