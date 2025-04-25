package client

import (
	"github.com/autobrr/tqm/config"
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

	SetUploadLimit(hash string, limit int64) error

	ShouldIgnore(*config.Torrent) (bool, error)
	ShouldRemove(*config.Torrent) (bool, error)
	CheckTorrentPause(*config.Torrent) (bool, error)
	ShouldRelabel(*config.Torrent) (string, bool, error)

	PauseTorrents([]string) error
}
