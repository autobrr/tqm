package torrentfilemap

import (
	"sort"
	"strings"
	"sync"

	"github.com/autobrr/tqm/pkg/config"
)

func New(torrents map[string]config.Torrent) *TorrentFileMap {
	// Pre-allocate map capacity: estimate average 10 files per torrent
	// This reduces map growth/reallocation overhead during initialization
	estimatedCapacity := len(torrents) * 10

	tfm := &TorrentFileMap{
		torrentFileMap: make(map[string]map[string]config.Torrent, estimatedCapacity),
		pathCache:      sync.Map{},
		mappingCache:   newMappingCacheManager(),
		indexDirty:     true,
	}

	tfm.mu.Lock()
	for _, torrent := range torrents {
		tfm.addInternal(torrent)
	}
	// Build initial index after all torrents are added
	tfm.rebuildIndexInternal()
	tfm.mu.Unlock()

	return tfm
}

// addInternal is the non-locking version of Add for use within New
func (t *TorrentFileMap) addInternal(torrent config.Torrent) {
	for _, f := range torrent.Files {
		if _, exists := t.torrentFileMap[f]; exists {
			t.torrentFileMap[f][torrent.Hash] = torrent
			continue
		}

		t.torrentFileMap[f] = map[string]config.Torrent{
			torrent.Hash: torrent,
		}
	}
	t.indexDirty = true
}

// rebuildIndexInternal rebuilds the path index for fast binary search (non-locking)
// This should be called after batch modifications to torrentFileMap
func (t *TorrentFileMap) rebuildIndexInternal() {
	if !t.indexDirty {
		return
	}

	// Build sorted index of all paths for binary search
	t.pathIndex = make([]string, 0, len(t.torrentFileMap))
	for path := range t.torrentFileMap {
		t.pathIndex = append(t.pathIndex, path)
	}
	sort.Strings(t.pathIndex)
	t.indexDirty = false
}

func (t *TorrentFileMap) Add(torrent config.Torrent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, f := range torrent.Files {
		if _, exists := t.torrentFileMap[f]; exists {
			// filepath already associated with other torrents
			t.torrentFileMap[f][torrent.Hash] = torrent
			continue
		}

		// filepath has not been seen before, create file entry
		t.torrentFileMap[f] = map[string]config.Torrent{
			torrent.Hash: torrent,
		}
	}

	// Mark index as needing rebuild and rebuild it
	t.indexDirty = true
	t.rebuildIndexInternal()

	// Invalidate mapping cache since paths changed
	t.mappingCache.invalidate()
}

func (t *TorrentFileMap) Remove(torrent config.Torrent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	needsRebuild := false
	for _, f := range torrent.Files {
		if _, exists := t.torrentFileMap[f]; exists {
			// remove this hash from the file entry
			delete(t.torrentFileMap[f], torrent.Hash)

			// remove file entry if no more hashes
			if len(t.torrentFileMap[f]) == 0 {
				delete(t.torrentFileMap, f)
				needsRebuild = true
			}

			continue
		}
	}

	// Rebuild index if we removed any paths
	if needsRebuild {
		t.indexDirty = true
		t.rebuildIndexInternal()

		// Invalidate mapping cache since paths changed
		t.mappingCache.invalidate()
	}
}

func (t *TorrentFileMap) IsUnique(torrent config.Torrent) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, f := range torrent.Files {
		if torrents, exists := t.torrentFileMap[f]; exists && len(torrents) > 1 {
			return false
		}
	}

	return true
}

func (t *TorrentFileMap) NoInstances(torrent config.Torrent) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, f := range torrent.Files {
		if torrents, exists := t.torrentFileMap[f]; exists && len(torrents) >= 1 {
			return false
		}
	}

	return true
}

func (t *TorrentFileMap) HasPath(path string, torrentPathMapping map[string]string) bool {
	if val, found := t.pathCache.Load(path); found {
		return val.(bool)
	}

	t.mu.RLock()
	var found bool
	if len(torrentPathMapping) == 0 {
		found = t.hasPathDirect(path)
	} else {
		found = t.hasPathWithMapping(path, torrentPathMapping)
	}
	t.mu.RUnlock()

	t.pathCache.Store(path, found)

	return found
}

// hasPathDirect checks if a path exists directly (no mappings)
// Uses binary search on sorted path index for O(log n) performance
func (t *TorrentFileMap) hasPathDirect(path string) bool {
	// Binary search to find paths that could contain the search path
	// We use sort.Search to find the first path >= our search path
	idx := sort.Search(len(t.pathIndex), func(i int) bool {
		return t.pathIndex[i] >= path
	})

	// Check paths starting from the found index
	// Since paths are sorted, we can stop early if we pass the possible matches
	for i := idx; i < len(t.pathIndex); i++ {
		torrentPath := t.pathIndex[i]

		// If the torrent path doesn't start with any prefix of our search path,
		// and our search path doesn't start with any prefix of the torrent path,
		// we can stop searching (sorted order guarantees no more matches)
		if !strings.HasPrefix(torrentPath, path[:min(len(path), len(torrentPath))]) &&
			!strings.HasPrefix(path, torrentPath[:min(len(path), len(torrentPath))]) {
			break
		}

		if strings.Contains(torrentPath, path) {
			return true
		}
	}

	// Also check backwards from idx-1 for paths that might contain our search path
	for i := idx - 1; i >= 0; i-- {
		torrentPath := t.pathIndex[i]

		// Similar early termination logic
		if !strings.HasPrefix(torrentPath, path[:min(len(path), len(torrentPath))]) &&
			!strings.HasPrefix(path, torrentPath[:min(len(path), len(torrentPath))]) {
			break
		}

		if strings.Contains(torrentPath, path) {
			return true
		}
	}

	return false
}

// hasPathWithMapping checks if a path exists using torrent path mappings
// Uses lazy per-query caching to avoid repeated path transformations
func (t *TorrentFileMap) hasPathWithMapping(path string, torrentPathMapping map[string]string) bool {
	// Get or build cache for this mapping configuration
	cache := t.mappingCache.getOrBuild(t.pathIndex, torrentPathMapping)

	// Simple O(n) scan through mapped paths
	// This is faster than O(n*m) transformation on every call
	for _, mappedPath := range cache.mappedPaths {
		if strings.Contains(mappedPath, path) {
			return true
		}
	}

	return false
}

func (t *TorrentFileMap) RemovePath(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pathCache.Delete(path)
	if _, exists := t.torrentFileMap[path]; exists {
		delete(t.torrentFileMap, path)
		t.indexDirty = true
		t.rebuildIndexInternal()

		// Invalidate mapping cache since paths changed
		t.mappingCache.invalidate()
	}
}

func (t *TorrentFileMap) Length() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.torrentFileMap)
}
