package client

import (
	"github.com/autobrr/tqm/pkg/config"
)

type Interface interface {
	Type() string
	Connect() error
	GetTorrents() (map[string]config.Torrent, error)
	RemoveTorrent(string, bool) (bool, error)
	SetTorrentLabel(hash string, label string, hardlink bool) error
	GetCurrentFreeSpace(string) (int64, error)
	AddFreeSpace(int64)
	GetFreeSpace() float64
	LoadLabelPathMap() error
	LabelPathMap() map[string]string

	ShouldIgnore(*config.Torrent) (bool, error)
	ShouldRemove(*config.Torrent) (bool, error)
	ShouldRelabel(*config.Torrent) (string, bool, error)
}
