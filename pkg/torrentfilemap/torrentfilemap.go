package torrentfilemap

import (
	"strings"
	"sync"

	"github.com/autobrr/tqm/pkg/config"
)

func New(torrents map[string]config.Torrent) *TorrentFileMap {
	tfm := &TorrentFileMap{
		torrentFileMap: make(map[string]map[string]config.Torrent),
		pathCache:      sync.Map{},
	}

	tfm.mu.Lock()
	for _, torrent := range torrents {
		tfm.addInternal(torrent)
	}
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
}

func (t *TorrentFileMap) Remove(torrent config.Torrent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, f := range torrent.Files {
		if _, exists := t.torrentFileMap[f]; exists {
			// remove this hash from the file entry
			delete(t.torrentFileMap[f], torrent.Hash)

			// remove file entry if no more hashes
			if len(t.torrentFileMap[f]) == 0 {
				delete(t.torrentFileMap, f)
			}

			continue
		}
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
func (t *TorrentFileMap) hasPathDirect(path string) bool {
	for torrentPath := range t.torrentFileMap {
		if strings.Contains(torrentPath, path) {
			return true
		}
	}
	return false
}

// hasPathWithMapping checks if a path exists using torrent path mappings
func (t *TorrentFileMap) hasPathWithMapping(path string, torrentPathMapping map[string]string) bool {
	for torrentPath := range t.torrentFileMap {
		for mapFrom, mapTo := range torrentPathMapping {
			if strings.Contains(strings.Replace(torrentPath, mapFrom, mapTo, 1), path) {
				return true
			}
		}
	}
	return false
}

func (t *TorrentFileMap) RemovePath(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pathCache.Delete(path)
	delete(t.torrentFileMap, path)
}

func (t *TorrentFileMap) Length() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.torrentFileMap)
}

// HasPathInCategory checks if a local file path belongs to any torrent.
// For files: checks exact match against torrent file paths
// For directories: checks if the path is a torrent save path
func (t *TorrentFileMap) HasPathInCategory(localPath string, torrentPathMapping map[string]string, log interface{}) bool {
	if val, found := t.pathCache.Load(localPath); found {
		return val.(bool)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	// Normalize the local path for comparison
	normalizedLocalPath := strings.ToLower(strings.ReplaceAll(localPath, "\\", "/"))

	// Debug logging
	if log != nil {
		if logger, ok := log.(interface{ Tracef(string, ...interface{}) }); ok {
			logger.Tracef("Checking if path is tracked: %q", normalizedLocalPath)
		}
	}

	// First check if it's a file path in the torrent file map
	for torrentFilePath := range t.torrentFileMap {
		normalizedTorrentPath := strings.ToLower(strings.ReplaceAll(torrentFilePath, "\\", "/"))

		if normalizedTorrentPath == normalizedLocalPath {
			if log != nil {
				if logger, ok := log.(interface{ Tracef(string, ...interface{}) }); ok {
					logger.Tracef("Found exact match for file: %q", torrentFilePath)
				}
			}
			t.pathCache.Store(localPath, true)
			return true
		}

		// Also check with path mappings applied
		if len(torrentPathMapping) > 0 {
			for mapFrom, mapTo := range torrentPathMapping {
				mappedPath := strings.Replace(torrentFilePath, mapFrom, mapTo, 1)
				normalizedMappedPath := strings.ToLower(strings.ReplaceAll(mappedPath, "\\", "/"))

				if normalizedMappedPath == normalizedLocalPath {
					if log != nil {
						if logger, ok := log.(interface{ Tracef(string, ...interface{}) }); ok {
							logger.Tracef("Found mapped match for file: %q -> %q", torrentFilePath, mappedPath)
						}
					}
					t.pathCache.Store(localPath, true)
					return true
				}
			}
		}
	}

	// Check if it's a torrent save path (for directories)
	for _, torrents := range t.torrentFileMap {
		for _, torrent := range torrents {
			normalizedTorrentPath := strings.ToLower(strings.ReplaceAll(torrent.Path, "\\", "/"))

			if normalizedTorrentPath == normalizedLocalPath {
				if log != nil {
					if logger, ok := log.(interface{ Tracef(string, ...interface{}) }); ok {
						logger.Tracef("Found match for torrent save path: %q", torrent.Path)
					}
				}
				t.pathCache.Store(localPath, true)
				return true
			}

			// Also check with path mappings applied to torrent path
			if len(torrentPathMapping) > 0 {
				for mapFrom, mapTo := range torrentPathMapping {
					mappedPath := strings.Replace(torrent.Path, mapFrom, mapTo, 1)
					normalizedMappedPath := strings.ToLower(strings.ReplaceAll(mappedPath, "\\", "/"))

					if normalizedMappedPath == normalizedLocalPath {
						if log != nil {
							if logger, ok := log.(interface{ Tracef(string, ...interface{}) }); ok {
								logger.Tracef("Found mapped match for torrent save path: %q -> %q", torrent.Path, mappedPath)
							}
						}
						t.pathCache.Store(localPath, true)
						return true
					}
				}
			}
		}
		// Only check the first torrent since they should all have the same Path for the same hash
		break
	}

	if log != nil {
		if logger, ok := log.(interface{ Tracef(string, ...interface{}) }); ok {
			logger.Tracef("No match found for: %q", normalizedLocalPath)
		}
	}

	t.pathCache.Store(localPath, false)
	return false
}
