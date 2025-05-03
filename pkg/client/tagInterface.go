package client

import (
	"context"

	"github.com/autobrr/tqm/pkg/config"
)

type RetagInfo struct {
	Add      []string
	Remove   []string
	UploadKb *int64
}

type TagInterface interface {
	Interface

	ShouldRetag(ctx context.Context, t *config.Torrent) (RetagInfo, error)
	AddTags(ctx context.Context, hash string, tags []string) error
	RemoveTags(ctx context.Context, hash string, tags []string) error
	CreateTags(ctx context.Context, tags []string) error
	DeleteTags(ctx context.Context, tags []string) error
}
