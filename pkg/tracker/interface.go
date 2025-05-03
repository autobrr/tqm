package tracker

import "context"

type Interface interface {
	Name() string
	Check(host string) bool
	IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool)
	IsTrackerDown(torrent *Torrent) (error, bool)
}
