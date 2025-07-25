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

type OPSConfig struct {
	Key string `koanf:"api_key"`
}

type OPS struct {
	cfg     OPSConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewOPS(c OPSConfig) *OPS {
	l := logger.GetLogger("ops-api")
	return &OPS{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Accept":        "application/json",
			"Authorization": "token " + c.Key,
		},
		log: l,
	}
}

func (c *OPS) Name() string {
	return "OPS"
}

func (c *OPS) Check(host string) bool {
	return strings.Contains(host, "opsfet.ch")
}

func (c *OPS) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type response struct {
		Status   string `json:"status"`
		Error    string `json:"error"`
		Response any    `json:"response"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying OPS API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	requestURL, err := httputils.URLWithQuery("https://orpheus.network/ajax.php", url.Values{
		"action": []string{"torrent"},
		"hash":   []string{torrent.Hash},
	})
	if err != nil {
		return fmt.Errorf("creating request URL: %w", err), false
	}

	var resp *response
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodGet, requestURL, nil, c.headers, &resp)
	if err != nil {
		return fmt.Errorf("making api request: %w", err), false
	}

	return nil, resp.Status == "failure" && resp.Error == "bad parameters"
}

func (c *OPS) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
