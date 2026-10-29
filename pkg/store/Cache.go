package store

import "sync"

// objectCache holds references to existing objects keyed by their store key.
// It is guarded by mu for concurrent use.
type objectCache struct {
	mu    sync.RWMutex
	cache map[string]any
}

// SetCache stores a reference to an existing object in the cache under key.
func (c *objectCache) SetCache(key string, obj any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]any)
	}
	c.cache[key] = obj
}

// GetCache returns the cached object for key, or nil when not present.
func (c *objectCache) GetCache(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	obj, ok := c.cache[key]
	return obj, ok
}

// DeleteCache removes the cached object for key, reporting whether it existed.
func (c *objectCache) DeleteCache(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.cache[key]; !ok {
		return false
	}
	delete(c.cache, key)
	return true
}

// ClearCache removes all cached objects.
func (c *objectCache) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]any)
}
