package hardlinkfilemap

import "github.com/autobrr/tqm/config"

type HardlinkFileMapI interface {
	AddByTorrent(torrent config.Torrent)
	RemoveByTorrent(torrent config.Torrent)
	NoInstances(torrent config.Torrent) bool
	IsTorrentUnique(torrent config.Torrent) bool
	HardlinkedOutsideClient(torrent config.Torrent) bool
	Length() int
}
