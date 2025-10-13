package torrentfilemap

import (
	"sync"

	"github.com/autobrr/tqm/pkg/config"
)

type TorrentFileMap struct {
	torrentFileMap map[string]map[string]config.Torrent
	// pathIndex is a sorted slice of all torrent file paths for fast binary search
	// This eliminates O(n) linear scanning in HasPath() method
	pathIndex []string
	pathCache sync.Map
	// mappingCache stores precomputed mapped paths per mapping configuration
	// This avoids O(n*m) path transformations on every HasPath() call
	mappingCache *mappingCacheManager
	mu           sync.RWMutex
	// indexDirty tracks whether pathIndex needs rebuilding
	indexDirty bool
}
