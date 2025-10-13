package torrentfilemap

import (
	"sort"
	"strings"
	"sync"
)

// mappingCacheKey creates a stable key for a mapping configuration
// This allows us to cache mapped paths per mapping configuration
func mappingCacheKey(mapping map[string]string) string {
	if len(mapping) == 0 {
		return ""
	}

	// Create a stable string representation of the mapping
	// We need consistent ordering for cache hits
	keys := make([]string, 0, len(mapping))
	for k := range mapping {
		keys = append(keys, k)
	}

	// IMPORTANT: Sort keys for stable cache key
	// Without this, map iteration order is random and we'd get different cache keys
	sort.Strings(keys)

	var builder strings.Builder
	builder.Grow(len(mapping) * 50) // Estimate 50 chars per mapping

	for i, k := range keys {
		if i > 0 {
			builder.WriteByte('|')
		}
		builder.WriteString(k)
		builder.WriteByte('=')
		builder.WriteString(mapping[k])
	}

	return builder.String()
}

// mappedPathCache stores precomputed mapped paths for a specific mapping configuration
type mappedPathCache struct {
	// mappedPaths contains all torrent paths after applying mappings
	mappedPaths []string
	// originalToMapped maps original path -> mapped path for reverse lookup
	originalToMapped map[string]string
}

// buildMappedPathCache precomputes all mapped paths for a given mapping configuration
// This is called lazily the first time a mapping configuration is used
func buildMappedPathCache(torrentPaths []string, mapping map[string]string) *mappedPathCache {
	cache := &mappedPathCache{
		mappedPaths:      make([]string, 0, len(torrentPaths)),
		originalToMapped: make(map[string]string, len(torrentPaths)),
	}

	for _, torrentPath := range torrentPaths {
		mappedPath := torrentPath

		// Apply all mappings to this path
		for mapFrom, mapTo := range mapping {
			// Only replace first occurrence to match current behavior
			mappedPath = strings.Replace(mappedPath, mapFrom, mapTo, 1)
		}

		cache.mappedPaths = append(cache.mappedPaths, mappedPath)
		cache.originalToMapped[torrentPath] = mappedPath
	}

	return cache
}

// mappingCacheManager manages per-mapping-configuration caches
type mappingCacheManager struct {
	mu     sync.RWMutex
	caches map[string]*mappedPathCache
}

func newMappingCacheManager() *mappingCacheManager {
	return &mappingCacheManager{
		caches: make(map[string]*mappedPathCache),
	}
}

// getOrBuild returns a cached mapping or builds a new one
func (m *mappingCacheManager) getOrBuild(torrentPaths []string, mapping map[string]string) *mappedPathCache {
	key := mappingCacheKey(mapping)

	// Fast path: read lock
	m.mu.RLock()
	cache, exists := m.caches[key]
	m.mu.RUnlock()

	if exists {
		return cache
	}

	// Slow path: write lock and build
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if cache, exists := m.caches[key]; exists {
		return cache
	}

	// Build new cache
	cache = buildMappedPathCache(torrentPaths, mapping)
	m.caches[key] = cache

	return cache
}

// invalidate clears all cached mappings (called when torrent paths change)
func (m *mappingCacheManager) invalidate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.caches = make(map[string]*mappedPathCache)
}