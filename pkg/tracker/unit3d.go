package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lucperkins/rek"
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

// https://github.com/HDInnovations/UNIT3D-Community-Edition/wiki/Torrent-API-(UNIT3D-v8.3.4)
func NewUNIT3D(name string, c UNIT3DConfig) Interface {
	l := logger.GetLogger(fmt.Sprintf("%s-api", strings.ToLower(name)))

	return &UNIT3D{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack), l),
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
	if !strings.EqualFold(torrent.TrackerName, c.cfg.Domain) {
		return nil, false
	}

	if torrent.Comment == "" {
		c.log.Debugf("Skipping torrent check - no comment available (likely Deluge client): %s", torrent.Name)
		return nil, false
	}

	c.log.Infof("Checking torrent from %s: %s", torrent.TrackerName, torrent.Name)

	// extract torrent ID from comment
	torrentID, err := c.extractTorrentID(torrent.Comment)
	if err != nil {
		//c.log.Debugf("Skipping torrent check - %v", err)
		return nil, false
	}

	type TorrentData struct {
		Attributes struct {
			InfoHash string `json:"info_hash"`
		} `json:"attributes"`
	}

	type Response struct {
		Data    TorrentData `json:"data"`
		Message string      `json:"message"`
		Status  int         `json:"status"`
	}

	// prepare request
	url := fmt.Sprintf("https://%s/api/torrents/%s", c.cfg.Domain, torrentID)

	// send request
	resp, err := rek.Get(url,
		rek.Client(c.http),
		rek.Headers(c.headers),
		rek.Context(ctx),
	)
	if err != nil {
		if resp == nil {
			c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
			return fmt.Errorf("unit3d: request search: %w", err), false
		}
	}
	defer resp.Body().Close()

	if resp.StatusCode() == 404 {
		//c.log.Debugf("Torrent not found: %s (hash: %s)", torrent.Name, torrent.Hash)
		return nil, true
	}

	// validate other response codes
	if resp.StatusCode() != 200 {
		c.log.WithError(err).Errorf("Failed validating search response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("unit3d: validate search response: %s", resp.Status()), false
	}

	// decode response
	b := new(Response)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		c.log.WithError(err).Errorf("Failed decoding search response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("unit3d: decode search response: %w", err), false
	}

	// compare info hash
	if strings.EqualFold(b.Data.Attributes.InfoHash, torrent.Hash) {
		// torrent exists and hash matches
		return nil, false
	}

	// if we get here, the torrent ID exists but hash doesn't match
	c.log.Debugf("Torrent ID exists but hash mismatch. Expected: %s, Got: %s",
		torrent.Hash, b.Data.Attributes.InfoHash)
	return nil, true
}

func (c *UNIT3D) IsTrackerDown(torrent *Torrent) (error, bool) {
	return nil, false
}
