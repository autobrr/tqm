package torrentfilemap

import (
	"strings"
	"sync"

	"github.com/autobrr/tqm/config"
)

func New(torrents map[string]config.Torrent) *TorrentFileMap {
	tfm := &TorrentFileMap{
		torrentFileMap: make(map[string]map[string]config.Torrent),
		pathCache:      sync.Map{},
	}

	for _, torrent := range torrents {
		tfm.Add(torrent)
	}

	return tfm
}

func (t *TorrentFileMap) Add(torrent config.Torrent) {
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
	for _, f := range torrent.Files {
		if torrents, exists := t.torrentFileMap[f]; exists && len(torrents) > 1 {
			return false
		}
	}

	return true
}

func (t *TorrentFileMap) NoInstances(torrent config.Torrent) bool {
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

	if len(torrentPathMapping) == 0 {
		return t.hasPathDirect(path)
	}

	found := t.hasPathWithMapping(path, torrentPathMapping)

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
	delete(t.torrentFileMap, path)
}

func (t *TorrentFileMap) Length() int {
	return len(t.torrentFileMap)
}
