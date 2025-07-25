package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/httputils"
	"github.com/autobrr/tqm/pkg/logger"
)

type BHDConfig struct {
	Key string `koanf:"api_key"`
}

type BHD struct {
	cfg     BHDConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewBHD(c BHDConfig) *BHD {
	l := logger.GetLogger("bhd-api")
	return &BHD{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		log: l,
	}
}

func (c *BHD) Name() string {
	return "BHD"
}

func (c *BHD) Check(host string) bool {
	return strings.Contains(host, "beyond-hd.me")
}

func (c *BHD) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type request struct {
		Hash   string `json:"info_hash"`
		Action string `json:"action"`
	}

	type result struct {
		Name     string `json:"name"`
		InfoHash string `json:"info_hash"`
	}

	type response struct {
		StatusCode   int      `json:"status_code"`
		Page         int      `json:"page"`
		Results      []result `json:"results"`
		TotalPages   int      `json:"total_pages"`
		TotalResults int      `json:"total_results"`
		Success      bool     `json:"success"`
	}

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

	requestURL, err := url.JoinPath("https://beyond-hd.me/api/torrents", c.cfg.Key)
	if err != nil {
		return fmt.Errorf("creating request URL: %w", sanitizeError(err)), false
	}

	payload := &request{
		Hash:   torrent.Hash,
		Action: "search",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", sanitizeError(err)), false
	}

	var resp *response
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodPost, requestURL, bytes.NewReader(body), c.headers, &resp)
	if err != nil {
		return fmt.Errorf("making api request: %w", sanitizeError(err)), false
	}

	// verify API response structure
	if !resp.Success || resp.StatusCode == 0 || resp.Page == 0 {
		return fmt.Errorf("API error"), false
	}

	return nil, resp.TotalResults < 1
}

func (c *BHD) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
