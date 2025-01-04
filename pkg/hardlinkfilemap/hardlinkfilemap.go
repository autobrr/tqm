package hardlinkfilemap

import (
	"os"
	"strings"

	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/logger"

	"github.com/scylladb/go-set/strset"
)

func New(torrents map[string]config.Torrent, torrentPathMapping map[string]string) HardlinkFileMapI {
	tfm := &HardlinkFileMap{
		hardlinkFileMap:    make(map[string]*strset.Set),
		log:                logger.GetLogger("hardlinkfilemap"),
		torrentPathMapping: torrentPathMapping,
	}

	for _, torrent := range torrents {
		tfm.AddByTorrent(torrent)
	}

	return tfm
}

func (t *HardlinkFileMap) considerPathMapping(path string) string {
	for mapFrom, mapTo := range t.torrentPathMapping {
		if strings.HasPrefix(path, mapFrom) {
			return strings.Replace(path, mapFrom, mapTo, 1)
		}
	}

	return path
}

func (t *HardlinkFileMap) linkInfoByPath(path string) (string, uint64, bool) {
	stat, err1 := os.Stat(path)
	if err1 != nil {
		t.log.Warnf("Failed to stat file: %s - %s", path, err1)
		return "", 0, false
	}

	id, nlink, err2 := LinkInfo(stat, path)
	if err2 != nil {
		t.log.Warnf("Failed to get file identifier: %s - %s", path, err2)
		return "", 0, false
	}

	return id, nlink, true
}

func (t *HardlinkFileMap) AddByTorrent(torrent config.Torrent) {
	if !torrent.Downloaded {
		return
	}

	for _, f := range torrent.Files {
		f = t.considerPathMapping(f)

		id, _, ok := t.linkInfoByPath(f)

		if !ok {
			continue
		}

		if _, exists := t.hardlinkFileMap[id]; exists {
			// file id already associated with other paths
			t.hardlinkFileMap[id].Add(f)
			continue
		}

		// file id has not been seen before, create id entry
		t.hardlinkFileMap[id] = strset.New(f)
	}
}

func (t *HardlinkFileMap) RemoveByTorrent(torrent config.Torrent) {
	if !torrent.Downloaded {
		return
	}

	for _, f := range torrent.Files {
		f = t.considerPathMapping(f)

		id, _, ok := t.linkInfoByPath(f)

		if !ok {
			continue
		}

		if _, exists := t.hardlinkFileMap[id]; exists {
			// remove this path from the id entry
			t.hardlinkFileMap[id].Remove(f)

			// remove id entry if no more paths
			if t.hardlinkFileMap[id].Size() == 0 {
				delete(t.hardlinkFileMap, id)
			}

			continue
		}
	}
}

func (t *HardlinkFileMap) countLinks(f string) (inmap uint64, total uint64, ok bool) {
	f = t.considerPathMapping(f)
	id, nlink, ok := t.linkInfoByPath(f)

	if !ok {
		return 0, 0, false
	}

	if paths, exists := t.hardlinkFileMap[id]; exists {
		return uint64(paths.Size()), nlink, true
	}

	return 0, nlink, true
}

func (t *HardlinkFileMap) HardlinkedOutsideClient(torrent config.Torrent) bool {
	if !torrent.Downloaded {
		return false
	}

	for _, f := range torrent.Files {
		inmap, total, ok := t.countLinks(f)
		if !ok {
			continue
		}

		if total != inmap {
			return true
		}
	}

	return false
}

func (t *HardlinkFileMap) IsTorrentUnique(torrent config.Torrent) bool {
	if !torrent.Downloaded {
		return true
	}

	for _, f := range torrent.Files {
		c, _, ok := t.countLinks(f)
		if !ok {
			return false
		}

		if c > 1 {
			return false
		}
	}

	return true
}

func (t *HardlinkFileMap) NoInstances(torrent config.Torrent) bool {
	if !torrent.Downloaded {
		return true
	}

	for _, f := range torrent.Files {
		c, _, ok := t.countLinks(f)
		if !ok {
			return false
		}

		if c != 0 {
			return false
		}
	}

	return true
}

func (t *HardlinkFileMap) Length() int {
	return len(t.hardlinkFileMap)
}
