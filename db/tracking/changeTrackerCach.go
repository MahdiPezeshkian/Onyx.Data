package tracking

import (
	"fmt"
	"sync"
)

// cache stores serialized entity snapshots keyed by type and id.
type cache interface {
	Set(key string, value any) error
	Get(key string) (any, error)
}

// memoryCache is a process-local cache backed by a sync.Map. It performs no eviction;
// snapshots live for the lifetime of the tracker.
type memoryCache struct {
	data sync.Map
}

// newMemoryCache returns an empty in-memory cache.
func newMemoryCache() *memoryCache {
	return &memoryCache{data: sync.Map{}}
}

func (m *memoryCache) Set(key string, value any) error {
	m.data.Store(key, value)
	return nil
}

func (m *memoryCache) Get(key string) (any, error) {
	val, ok := m.data.Load(key)
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return val, nil
}
