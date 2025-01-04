package hardlinkfilemap

import (
	"github.com/scylladb/go-set/strset"
	"github.com/sirupsen/logrus"
)

type HardlinkFileMap struct {
	// hardlinkFileMap map[string]map[string]config.Torrent
	hardlinkFileMap    map[string]*strset.Set
	log                *logrus.Entry
	torrentPathMapping map[string]string
}
