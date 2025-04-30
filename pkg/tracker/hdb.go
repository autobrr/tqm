package tracker

import (
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

type HDBConfig struct {
	Username string `koanf:"username"`
	Passkey  string `koanf:"passkey"`
}

type HDB struct {
	cfg  HDBConfig
	http *http.Client
	log  *logrus.Entry
}

func NewHDB(c HDBConfig) *HDB {
	l := logger.GetLogger("hdb-api")
	return &HDB{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack), l),
		log:  l,
	}
}

func (c *HDB) Name() string {
	return "HDB"
}

func (c *HDB) Check(host string) bool {
	return strings.Contains(host, "hdbits.org")
}

func (c *HDB) IsUnregistered(torrent *Torrent) (error, bool) {
	//c.log.Infof("Checking HDB torrent: %s", torrent.Name)

	type Request struct {
		Username string `json:"username"`
		Passkey  string `json:"passkey"`
		Hash     string `json:"hash"`
	}

	type TorrentResult struct {
		ID   int    `json:"id"`
		Hash string `json:"hash"`
		Name string `json:"name"`
	}

	type Response struct {
		Status  int             `json:"status"`
		Message string          `json:"message"`
		Data    []TorrentResult `json:"data"`
	}

	// prepare request body
	reqBody := Request{
		Username: c.cfg.Username,
		Passkey:  c.cfg.Passkey,
		Hash:     strings.ToUpper(torrent.Hash),
	}

	// send request
	resp, err := rek.Post("https://hdbits.org/api/torrents",
		rek.Client(c.http),
		rek.Json(reqBody),
	)
	if err != nil {
		if resp == nil {
			c.log.WithError(err).Errorf("Failed searching for %s (hash: %s)", torrent.Name, torrent.Hash)
			return fmt.Errorf("hdb: request search: %w", err), false
		}
	}
	defer resp.Body().Close()

	// validate response
	if resp.StatusCode() != 200 {
		c.log.WithError(err).Errorf("Failed validating search response for %s (hash: %s), response: %s",
			torrent.Name, torrent.Hash, resp.Status())
		return fmt.Errorf("hdb: validate search response: %s", resp.Status()), false
	}

	// decode response
	b := new(Response)
	if err := json.NewDecoder(resp.Body()).Decode(b); err != nil {
		c.log.WithError(err).Errorf("Failed decoding search response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("hdb: decode search response: %w", err), false
	}

	// HDB returns status 0 for success, anything else is an error
	// if we get no results for a valid hash, the torrent is unregistered
	return nil, b.Status == 0 && len(b.Data) == 0
}

func (c *HDB) IsTrackerDown(torrent *Torrent) (error, bool) {
	return nil, false
}
