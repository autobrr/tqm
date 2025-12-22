package client

import (
	"context"

	"github.com/autobrr/tqm/pkg/config"
)

type Interface interface {
	Type() string
	Connect(ctx context.Context) error
	GetTorrents(ctx context.Context) (map[string]config.Torrent, error)
	RemoveTorrent(ctx context.Context, torrent *config.Torrent, deleteData bool) (bool, error)
	SetTorrentLabel(ctx context.Context, hash string, label string, hardlink bool) error
	GetCurrentFreeSpace(ctx context.Context, path string) (int64, error)
	AddFreeSpace(int64)
	GetFreeSpace() float64
	LoadLabelPathMap(ctx context.Context) error
	LabelPathMap() map[string]string

	SetUploadLimit(ctx context.Context, hash string, limit int64) error

	ShouldIgnore(ctx context.Context, t *config.Torrent) (bool, error)
	ShouldRemove(ctx context.Context, t *config.Torrent) (bool, error)
	ShouldRemoveWithReason(ctx context.Context, t *config.Torrent) (bool, string, error)
	CheckTorrentPause(ctx context.Context, t *config.Torrent) (bool, error)
	EvaluatePriority(ctx context.Context, t *config.Torrent) (float64, error)
	ShouldRelabel(ctx context.Context, t *config.Torrent) (string, bool, error)

	PauseTorrents(ctx context.Context, hashes []string) error
}
