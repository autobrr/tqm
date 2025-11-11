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

func (t *TorrentFileMap) GetTorrentsSharingFiles(torrent config.Torrent, allTorrents map[string]config.Torrent) ([]config.Torrent, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	groupHashes := make(map[string]bool)
	groupHashes[torrent.Hash] = true

	for _, f := range torrent.Files {
		if torrents, exists := t.torrentFileMap[f]; exists {
			for hash := range torrents {
				groupHashes[hash] = true
			}
		}
	}

	var group []config.Torrent
	for hash := range groupHashes {
		group = append(group, allTorrents[hash])
	}
	return group, nil
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
