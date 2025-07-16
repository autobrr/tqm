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

type REDConfig struct {
	Key string `koanf:"api_key"`
}

type RED struct {
	cfg     REDConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewRED(c REDConfig) *RED {
	l := logger.GetLogger("red-api")
	return &RED{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Accept":        "application/json",
			"Authorization": "token " + c.Key,
		},
		log: l,
	}
}

func (c *RED) Name() string {
	return "RED"
}

func (c *RED) Check(host string) bool {
	return strings.Contains(host, "flacsfor.me")
}

func (c *RED) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type response struct {
		Status   string `json:"status"`
		Error    string `json:"error"`
		Response any    `json:"response"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying RED API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// prepare request
	requestURL, err := httputils.URLWithQuery("https://redacted.sh/ajax.php", url.Values{
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

	return nil, resp.Status == "failure" && resp.Error == "bad hash parameter"
}

func (c *RED) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
