package tracker

import (
	"context"
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
			"Accept":  "application/json",
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
	type response struct {
		Result        string `json:"Result"`
		ResultDetails string `json:"ResultDetails"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying PTP API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// prepare request
	requestURL, err := httputils.URLWithQuery("https://passthepopcorn.me/torrents.php", url.Values{
		"infohash": []string{torrent.Hash},
	})
	if err != nil {
		return fmt.Errorf("creating request URL: %w", err), false
	}

	var resp *response
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodGet, requestURL, nil, c.headers, &resp)
	if err != nil {
		return fmt.Errorf("making api request: %w", err), false
	}

	return nil, resp.Result == "ERROR" && resp.ResultDetails == "Unregistered Torrent"
}

func (c *PTP) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
