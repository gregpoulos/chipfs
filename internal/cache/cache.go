// Package cache provides an LRU in-memory store for fully-rendered WAV track data.
//
// When a track is first requested, the emulator renders it entirely into memory.
// Subsequent reads—including backward seeks—are served from this cache instantly,
// avoiding re-emulation. Entries are evicted in least-recently-used order when
// the total cached size exceeds the configured capacity.
package cache

import (
	"container/list"
	"fmt"
	"sync"
)

// key uniquely identifies a cached track.
type key struct {
	sourcePath string
	trackIndex int
}

// entry is the value stored in each list element.
type entry struct {
	k    key
	data []byte
}

// Cache is a thread-safe LRU store keyed by (source file path, track index).
type Cache struct {
	mu       sync.Mutex
	maxBytes int64
	used     int64
	items    map[key]*list.Element
	lru      *list.List // front = MRU, back = LRU
}

// New creates a Cache with the given maximum byte capacity.
func New(maxBytes int64) *Cache {
	return &Cache{
		maxBytes: maxBytes,
		items:    make(map[key]*list.Element),
		lru:      list.New(),
	}
}

// Get retrieves the cached WAV data for a given source file path and track index.
// Returns (nil, false) on a cache miss. A hit promotes the entry to MRU.
func (c *Cache) Get(sourcePath string, trackIndex int) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key{sourcePath, trackIndex}]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*entry).data, true
}

// Set stores WAV data for a given source file path and track index.
// If the key already exists its value is replaced. After insertion, LRU entries
// are evicted until used bytes fit within capacity, keeping the new entry.
func (c *Cache) Set(sourcePath string, trackIndex int, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := key{sourcePath, trackIndex}

	// Remove existing entry for this key to avoid double-counting its bytes.
	if el, ok := c.items[k]; ok {
		c.used -= int64(len(el.Value.(*entry).data))
		c.lru.Remove(el)
		delete(c.items, k)
	}

	el := c.lru.PushFront(&entry{k, data})
	c.items[k] = el
	c.used += int64(len(data))

	// Evict from the back until we're within capacity, but never evict the
	// entry we just inserted (which is at the front).
	for c.used > c.maxBytes && c.lru.Len() > 1 {
		back := c.lru.Back()
		e := back.Value.(*entry)
		c.used -= int64(len(e.data))
		c.lru.Remove(back)
		delete(c.items, e.k)
	}
}

// String returns a human-readable summary of cache state (useful for debugging).
func (c *Cache) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fmt.Sprintf("Cache{entries: %d, used: %d, max: %d}", len(c.items), c.used, c.maxBytes)
}
