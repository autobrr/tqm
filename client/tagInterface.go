package client

import (
	"github.com/autobrr/tqm/config"
)

type RetagInfo struct {
	Add      []string
	Remove   []string
	UploadKb *int64
}

type TagInterface interface {
	Interface

	ShouldRetag(*config.Torrent) (RetagInfo, error)
	AddTags(string, []string) error
	RemoveTags(string, []string) error
	CreateTags([]string) error
	DeleteTags([]string) error
}
