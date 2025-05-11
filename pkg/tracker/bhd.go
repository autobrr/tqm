package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lucperkins/rek"
	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/httputils"
	"github.com/autobrr/tqm/pkg/logger"
)

type BHDConfig struct {
	Key string `koanf:"api_key"`
}

type BHD struct {
	cfg  BHDConfig
	http *http.Client
	log  *logrus.Entry
}

type BHDAPIRequest struct {
	Hash   string `json:"info_hash"`
	Action string `json:"action"`
}

type BHDAPIResponse struct {
	StatusCode int `json:"status_code"`
	Page       int `json:"page"`
	Results    []struct {
		Name     string `json:"name"`
		InfoHash string `json:"info_hash"`
	} `json:"results"`
	TotalPages   int  `json:"total_pages"`
	TotalResults int  `json:"total_results"`
	Success      bool `json:"success"`
}

func NewBHD(c BHDConfig) *BHD {
	l := logger.GetLogger("bhd-api")
	return &BHD{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		log:  l,
	}
}

func (c *BHD) Name() string {
	return "BHD"
}

func (c *BHD) Check(host string) bool {
	return strings.Contains(host, "beyond-hd.me")
}

func (c *BHD) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	// prepare request
	url := httputils.Join("https://beyond-hd.me/api/torrents", c.cfg.Key)
	payload := &BHDAPIRequest{
		Hash:   torrent.Hash,
		Action: "search",
	}

	// Log API request details
	c.log.Debugf("BHD API request for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// send request
	resp, err := rek.Post(url, rek.Client(c.http), rek.Json(payload), rek.Context(ctx))
	if err != nil {
		c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
		return fmt.Errorf("bhd: request search: %w", err), false
	}
	defer resp.Body().Close()

	// Check HTTP status code
	if resp.StatusCode() != 200 {
		c.log.Errorf("Failed API response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("bhd: non-200 response: %s", resp.Status()), false
	}

	// Read and parse the response
	b := new(BHDAPIResponse)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		// This covers both JSON parse errors (including HTML responses) in one check
		c.log.WithError(err).Errorf("Failed decoding response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("bhd: decode response: %w", err), false
	}

	// Verify API response structure
	if !b.Success || b.StatusCode == 0 || b.Page == 0 {
		c.log.Errorf("Invalid API response for %s (hash: %s): success=%t, status_code=%d, page=%d",
			torrent.Name, torrent.Hash, b.Success, b.StatusCode, b.Page)
		return fmt.Errorf("bhd: invalid API response"), false
	}

	// Final determination
	isUnregistered := b.TotalResults < 1
	if isUnregistered {
		c.log.Infof("BHD API confirms torrent is UNREGISTERED: %s (hash: %s)",
			torrent.Name, torrent.Hash)
	} else {
		c.log.Debugf("BHD API confirms torrent is registered: %s (hash: %s)",
			torrent.Name, torrent.Hash)
	}

	return nil, isUnregistered
}

func (c *BHD) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
