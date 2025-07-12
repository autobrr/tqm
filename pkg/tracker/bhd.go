package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucperkins/rek"
	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/http"
	"github.com/autobrr/tqm/pkg/logger"
)

type BHDConfig struct {
	Key string `koanf:"api_key"`
}

type BHD struct {
	cfg  BHDConfig
	http *nethttp.Client
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
		http: http.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
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
	requestURL, _ := url.JoinPath("https://beyond-hd.me/api/torrents", c.cfg.Key)
	payload := &BHDAPIRequest{
		Hash:   torrent.Hash,
		Action: "search",
	}

	// Log API request details
	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}
	c.log.Tracef("Querying BHD API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// Helper function to sanitize errors that might contain the API key
	sanitizeError := func(err error) error {
		if err == nil {
			return nil
		}
		errorMsg := err.Error()
		if c.cfg.Key != "" && strings.Contains(errorMsg, c.cfg.Key) {
			// Replace the API key with a placeholder
			sanitized := strings.ReplaceAll(errorMsg, c.cfg.Key, "[API_KEY_REDACTED]")
			return fmt.Errorf("%s", sanitized)
		}
		return err
	}

	// send request
	resp, err := rek.Post(requestURL, rek.Client(c.http), rek.Json(payload), rek.Context(ctx))
	if err != nil {
		safeErr := sanitizeError(err)
		c.log.WithError(safeErr).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
		return fmt.Errorf("bhd: request search: %w", safeErr), false
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
		safeErr := sanitizeError(err)
		c.log.WithError(safeErr).Errorf("Failed decoding response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("bhd: decode response: %w", safeErr), false
	}

	// Verify API response structure
	if !b.Success || b.StatusCode == 0 || b.Page == 0 {
		c.log.Errorf("Invalid API response for %s (hash: %s): success=%t, status_code=%d, page=%d",
			torrent.Name, torrent.Hash, b.Success, b.StatusCode, b.Page)
		return fmt.Errorf("bhd: invalid API response"), false
	}

	return nil, b.TotalResults < 1
}

func (c *BHD) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
