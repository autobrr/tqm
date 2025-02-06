package tracker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/autobrr/tqm/httputils"
	"github.com/autobrr/tqm/logger"
	"github.com/lucperkins/rek"
	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"
)

type OPSConfig struct {
	Key string `koanf:"api_key"`
}

type OPS struct {
	cfg  OPSConfig
	http *http.Client
	log  *logrus.Entry
}

func NewOPS(c OPSConfig) *OPS {
	l := logger.GetLogger("ops-api")
	return &OPS{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack), l),
		log:  l,
	}
}

func (c *OPS) Name() string {
	return "OPS"
}

func (c *OPS) Check(host string) bool {
	return strings.Contains(host, "opsfet.ch")
}

func (c *OPS) IsUnregistered(torrent *Torrent) (error, bool) {
	//c.log.Infof("Checking OPS torrent: %s", torrent.Name)

	type Response struct {
		Status   string      `json:"status"`
		Error    string      `json:"error"`
		Response interface{} `json:"response"`
		Info     struct {
			Source  string `json:"source"`
			Version int    `json:"version"`
		} `json:"info"`
	}

	// prepare request
	url := fmt.Sprintf("https://orpheus.network/ajax.php?action=torrent&hash=%s", strings.ToUpper(torrent.Hash))

	// send request with API key in Authorization header
	resp, err := rek.Get(url,
		rek.Client(c.http),
		rek.Headers(map[string]string{
			"Authorization": fmt.Sprintf("token %s", c.cfg.Key),
		}),
	)
	if err != nil {
		if resp == nil {
			c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
			return fmt.Errorf("ops: request search: %w", err), false
		}
	}
	defer resp.Body().Close()

	// validate response
	if resp.StatusCode() != 200 {
		c.log.WithError(err).Errorf("Failed validating search response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("ops: validate search response: %s", resp.Status()), false
	}

	// decode response
	b := new(Response)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		c.log.WithError(err).Errorf("Failed decoding search response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("ops: decode search response: %w", err), false
	}

	return nil, b.Status == "failure" && b.Error == "bad parameters"
}

func (c *OPS) IsTrackerDown(torrent *Torrent) (error, bool) {
	return nil, false
}
