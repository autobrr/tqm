package tracker

type Interface interface {
	Name() string
	Check(string) bool
	IsUnregistered(torrent *Torrent) (error, bool)
}
