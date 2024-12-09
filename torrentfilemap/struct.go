package torrentfilemap

import (
	"github.com/autobrr/tqm/config"
)

type TorrentFileMap struct {
	torrentFileMap map[string]map[string]config.Torrent
}
