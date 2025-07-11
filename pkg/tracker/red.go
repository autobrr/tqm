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

type REDConfig struct {
	Key string `koanf:"api_key"`
}

type RED struct {
	cfg  REDConfig
	http *http.Client
	log  *logrus.Entry
}

func NewRED(c REDConfig) *RED {
	l := logger.GetLogger("red-api")
	return &RED{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		log:  l,
	}
}

func (c *RED) Name() string {
	return "RED"
}

func (c *RED) Check(host string) bool {
	return strings.Contains(host, "flacsfor.me")
}

func (c *RED) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	//c.log.Infof("Checking RED torrent: %s", torrent.Name)

	type Response struct {
		Status   string `json:"status"`
		Error    string `json:"error"`
		Response struct {
			Group   interface{} `json:"group"`
			Torrent struct {
				ID       int    `json:"id"`
				InfoHash string `json:"infoHash"`
			} `json:"torrent"`
		} `json:"response"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying RED API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// prepare request
	url := fmt.Sprintf("https://redacted.sh/ajax.php?action=torrent&hash=%s", strings.ToUpper(torrent.Hash))

	// send request with API key in header
	resp, err := rek.Get(url,
		rek.Client(c.http),
		rek.Headers(map[string]string{
			"Authorization": c.cfg.Key,
		}),
		rek.Context(ctx),
	)
	if err != nil {
		if resp == nil {
			c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
			return fmt.Errorf("redacted: request search: %w", err), false
		}
	}
	defer resp.Body().Close()

	// validate response
	if resp.StatusCode() != 200 && resp.StatusCode() != 400 {
		c.log.WithError(err).Errorf("Failed validating search response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("redacted: validate search response: %s", resp.Status()), false
	}

	// decode response
	b := new(Response)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		c.log.WithError(err).Errorf("Failed decoding search response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("redacted: decode search response: %w", err), false
	}

	return nil, b.Status == "failure" && b.Error == "bad hash parameter"
}

func (c *RED) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
