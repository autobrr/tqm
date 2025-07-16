package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	cfg     HDBConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewHDB(c HDBConfig) *HDB {
	l := logger.GetLogger("hdb-api")
	return &HDB{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		log: l,
	}
}

func (c *HDB) Name() string {
	return "HDB"
}

func (c *HDB) Check(host string) bool {
	return strings.Contains(host, "hdbits.org")
}

func (c *HDB) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type request struct {
		Username string `json:"username"`
		Passkey  string `json:"passkey"`
		Hash     string `json:"hash"`
	}

	type data struct {
		ID   int    `json:"id"`
		Hash string `json:"hash"`
		Name string `json:"name"`
	}

	type response struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Data    []data `json:"data"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying HDB API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	// prepare request body
	payload := &request{
		Username: c.cfg.Username,
		Passkey:  c.cfg.Passkey,
		Hash:     strings.ToUpper(torrent.Hash),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err), false
	}

	var resp *response
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodPost, "https://hdbits.org/api/torrents", bytes.NewReader(body), c.headers, &resp)
	if err != nil {
		return fmt.Errorf("making api request: %w", err), false
	}

	// HDB returns status 0 for success, anything else is an error
	// if we get no results for a valid hash, the torrent is unregistered
	return nil, resp.Status == 0 && len(resp.Data) == 0
}

func (c *HDB) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
