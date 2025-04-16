package client

import (
	"github.com/autobrr/tqm/config"
)

// RetagInfo holds the tags to add/remove and an optional upload speed limit
// determined by matching tag rules.
type RetagInfo struct {
	Add      []string
	Remove   []string
	UploadKb *int64
}

type TagInterface interface {
	Interface

	// ShouldRetag evaluates tag rules against a torrent.
	// It returns RetagInfo indicating tags to add/remove and potentially an upload limit
	// from the first matching rule that specifies one.
	// Returns an error if evaluation fails.
	ShouldRetag(*config.Torrent) (RetagInfo, error)
	AddTags(string, []string) error
	RemoveTags(string, []string) error
	CreateTags([]string) error
	DeleteTags([]string) error
}
