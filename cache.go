package main

import (
	"sync"
	"time"

	"github.com/raine/telegram-tori-bot/tori"
)

// FilterCache provides thread-safe caching for newad filters with TTL support.
type FilterCache struct {
	mu      sync.RWMutex
	data    *tori.NewadFilters
	expires time.Time
	ttl     time.Duration
}

// NewFilterCache creates a new filter cache with the specified TTL.
func NewFilterCache(ttl time.Duration) *FilterCache {
	return &FilterCache{
		ttl: ttl,
	}
}

// Get returns the cached filters if they exist and haven't expired.
func (c *FilterCache) Get() (tori.NewadFilters, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil || time.Now().After(c.expires) {
		return tori.NewadFilters{}, false
	}
	return *c.data, true
}

// Set stores the filters in the cache with the configured TTL.
func (c *FilterCache) Set(filters tori.NewadFilters) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = &filters
	c.expires = time.Now().Add(c.ttl)
}

// Clear removes any cached data.
func (c *FilterCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = nil
}
