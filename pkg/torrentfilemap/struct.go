package torrentfilemap

import (
	"github.com/autobrr/tqm/pkg/config"
)

type TorrentFileMap struct {
	torrentFileMap map[string]map[string]config.Torrent
}
