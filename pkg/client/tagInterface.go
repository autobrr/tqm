package client

import (
	"github.com/autobrr/tqm/pkg/config"
)

type RetagInfo struct {
	Add    []string
	Remove []string
}

type TagInterface interface {
	Interface

	ShouldRetag(*config.Torrent) (RetagInfo, bool, error)
	AddTags(string, []string) error
	RemoveTags(string, []string) error
	CreateTags([]string) error
	DeleteTags([]string) error
}
