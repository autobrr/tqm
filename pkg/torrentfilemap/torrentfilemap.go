package torrentfilemap

import (
	"strings"

	"github.com/autobrr/tqm/pkg/config"
)

func New(torrents map[string]config.Torrent) *TorrentFileMap {
	tfm := &TorrentFileMap{
		torrentFileMap: make(map[string]map[string]config.Torrent),
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
	// contains check
	for torrentPath := range t.torrentFileMap {
		// no torrent path mapping provided
		if len(torrentPathMapping) == 0 {
			if strings.Contains(torrentPath, path) {
				return true
			}

			continue
		}

		// iterate mappings checking
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
