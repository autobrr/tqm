package torrentfilemap

import (
	"sync"

	"github.com/autobrr/tqm/pkg/config"
)

type TorrentFileMap struct {
	torrentFileMap map[string]map[string]config.Torrent
	pathCache      sync.Map
	mu             sync.RWMutex
}
