package tracker

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/httputils"
	"github.com/autobrr/tqm/pkg/logger"
)

type UNIT3DConfig struct {
	APIKey string `koanf:"api_key"`
	Domain string `koanf:"domain"`
}

type UNIT3D struct {
	cfg     UNIT3DConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

// API docs: https://hdinnovations.github.io/UNIT3D/torrent_api.html
func NewUNIT3D(name string, c UNIT3DConfig) Interface {
	l := logger.GetLogger(fmt.Sprintf("%s-api", strings.ToLower(name)))

	return &UNIT3D{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Authorization": fmt.Sprintf("Bearer %s", c.APIKey),
			"Accept":        "application/json",
		},
		log: l,
	}
}

func (c *UNIT3D) Name() string {
	return "UNIT3D"
}

func (c *UNIT3D) Check(host string) bool {
	return strings.EqualFold(host, c.cfg.Domain)
}

// extractTorrentID extracts the torrent ID from the comment field
// example comment: "This torrent was downloaded from aither.cc. https://aither.cc/torrents/123456"
func (c *UNIT3D) extractTorrentID(comment string) (string, error) {
	if comment == "" {
		return "", fmt.Errorf("empty comment field")
	}

	// extract torrent ID from any URL in the comment that matches our domain
	re := regexp.MustCompile(fmt.Sprintf(`https?://[^/]*%s/(?:torrents|details)/(\d+)`, regexp.QuoteMeta(c.cfg.Domain)))
	matches := re.FindStringSubmatch(comment)

	if len(matches) < 2 {
		return "", fmt.Errorf("no torrent ID found in comment: %s", comment)
	}

	return matches[1], nil
}

func (c *UNIT3D) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type data struct {
		Attributes struct {
			InfoHash string `json:"info_hash"`
		} `json:"attributes"`
	}

	type response struct {
		Data    data   `json:"data"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying UNIT3D API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	if !strings.EqualFold(torrent.TrackerName, c.cfg.Domain) {
		return nil, false
	}

	torrentID, err := c.extractTorrentID(torrent.Comment)
	if err != nil {
		return nil, false
	}

	requestURL := fmt.Sprintf("https://%s/api/torrents/%s", c.cfg.Domain, torrentID)

	var resp *response
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodGet, requestURL, nil, c.headers, &resp)
	if err != nil {
		return fmt.Errorf("making api request: %w", err), false
	}

	// compare hash
	if strings.EqualFold(resp.Data.Attributes.InfoHash, torrent.Hash) {
		// torrent exists and hash matches
		return nil, false
	}

	// if we get here, the torrent ID exists but hash doesn't match
	c.log.Debugf("Torrent ID exists but hash mismatch. Expected: %s, Got: %s",
		torrent.Hash, resp.Data.Attributes.InfoHash)
	return nil, true
}

func (c *UNIT3D) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
