package torrentfilemap

import (
	"sync"

	"github.com/autobrr/tqm/config"
)

type TorrentFileMap struct {
	torrentFileMap map[string]map[string]config.Torrent
	pathCache      sync.Map
	mu             sync.RWMutex
}
