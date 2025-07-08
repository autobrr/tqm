package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucperkins/rek"
	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/httputils"
	"github.com/autobrr/tqm/pkg/logger"
)

type PTPConfig struct {
	User string `koanf:"api_user"`
	Key  string `koanf:"api_key"`
}

type PTP struct {
	cfg     PTPConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewPTP(c PTPConfig) *PTP {
	l := logger.GetLogger("ptp-api")
	return &PTP{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"ApiUser": c.User,
			"ApiKey":  c.Key,
		},
		log: l,
	}
}

func (c *PTP) Name() string {
	return "PTP"
}

func (c *PTP) Check(host string) bool {
	return strings.Contains(host, "passthepopcorn.me")
}

func (c *PTP) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type Response struct {
		Result        string `json:"Result"`
		ResultDetails string `json:"ResultDetails"`
	}

	c.log.Tracef("Querying PTP API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// prepare request
	reqURL, err := httputils.WithQuery("https://passthepopcorn.me/torrents.php", url.Values{
		"infohash": []string{torrent.Hash},
	})
	if err != nil {
		return fmt.Errorf("ptp: url parse: %w", err), false
	}

	// send request
	resp, err := rek.Get(reqURL, rek.Client(c.http), rek.Headers(c.headers), rek.Context(ctx))
	if err != nil {
		c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
		return fmt.Errorf("ptp: request search: %w", err), false
	}
	defer resp.Body().Close()

	// validate response
	if resp.StatusCode() != 200 {
		c.log.WithError(err).Errorf("Failed validating search response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("ptp: validate search response: %s", resp.Status()), false
	}

	// decode response
	b := new(Response)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		c.log.WithError(err).Errorf("Failed decoding search response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("ptp: decode search response: %w", err), false
	}

	return nil, b.Result == "ERROR" && b.ResultDetails == "Unregistered Torrent"
}

func (c *PTP) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
