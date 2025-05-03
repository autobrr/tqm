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

func NewBHD(c BHDConfig) *BHD {
	l := logger.GetLogger("bhd-api")
	return &BHD{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack), l),
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
	type Request struct {
		Hash   string `json:"info_hash"`
		Action string `json:"action"`
	}

	type Response struct {
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

	// prepare request
	url := httputils.Join("https://beyond-hd.me/api/torrents", c.cfg.Key)
	payload := &Request{
		Hash:   torrent.Hash,
		Action: "search",
	}

	// send request
	resp, err := rek.Post(url, rek.Client(c.http), rek.Json(payload), rek.Context(ctx))
	if err != nil {
		c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
		return fmt.Errorf("bhd: request search: %w", err), false
	}
	defer resp.Body().Close()

	// validate response
	if resp.StatusCode() != 200 {
		c.log.WithError(err).Errorf("Failed validating search response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("bhd: validate search response: %s", resp.Status()), false
	}

	// decode response
	b := new(Response)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		c.log.WithError(err).Errorf("Failed decoding search response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("bhd: decode search response: %w", err), false
	}

	return nil, b.TotalResults < 1
}

func (c *BHD) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
