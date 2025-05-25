package notification

import (
	"time"

	"github.com/autobrr/tqm/pkg/config"
)

type Action int

const (
	ActionRetag Action = iota + 1
	ActionRelabel
	ActionClean
	ActionPause
	ActionOrphan
)

type Sender interface {
	CanSend() bool
	Send(title string, description string, runTime time.Duration, fields []Field) error
	BuildField(action Action, options BuildOptions) Field
	Name() string
}

type Field struct {
	Name  string
	Value string
}

type BuildOptions struct {
	Torrent config.Torrent

	NewTags    []string
	NewUpLimit int64

	NewLabel string

	Orphan     string
	OrphanSize int64
	IsFile     bool
}
