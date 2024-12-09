package hardlinkfilemap

import "github.com/autobrr/tqm/config"

type noopHardlinkFileMap struct {
}

func NewNoopHardlinkFileMap() *noopHardlinkFileMap {
	return &noopHardlinkFileMap{}
}

func (h *noopHardlinkFileMap) AddByTorrent(torrent config.Torrent) {
}

func (h *noopHardlinkFileMap) RemoveByTorrent(torrent config.Torrent) {
}

func (h *noopHardlinkFileMap) NoInstances(torrent config.Torrent) bool {
	return true
}

func (h *noopHardlinkFileMap) IsTorrentUnique(torrent config.Torrent) bool {
	return true
}

func (h *noopHardlinkFileMap) HardlinkedOutsideClient(torrent config.Torrent) bool {
	return false
}

func (h *noopHardlinkFileMap) Length() int {
	return 0
}
