package tracker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	cfg                  PTPConfig
	http                 *http.Client
	headers              map[string]string
	log                  *logrus.Entry
	unregisteredCache    map[string]bool
	unregisteredFetched  bool
	unregisteredCacheMux sync.RWMutex
	apiError bool
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
		log:               l,
		unregisteredCache: make(map[string]bool),
	}
}

func (c *PTP) Name() string {
	return "PTP"
}

func (c *PTP) Check(host string) bool {
	return strings.Contains(host, "passthepopcorn.me")
}

func (c *PTP) fetchUnregisteredTorrents(ctx context.Context) error {
	type unregisteredResponse struct {
		Total        int `json:"Total"`
		Page         int `json:"Page"`
		Pages        int `json:"Pages"`
		Unregistered []struct {
			InfoHash string `json:"InfoHash"`
		} `json:"Unregistered"`
	}

	c.log.Debug("Fetching all unregistered torrents from PTP")

	requestURL, err := httputils.URLWithQuery("https://passthepopcorn.me/userhistory.php", url.Values{
		"action": []string{"unregistered"},
		"type":   []string{"json"},
	})
	if err != nil {
		return fmt.Errorf("creating request URL: %w", err)
	}

	var resp *unregisteredResponse
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodGet, requestURL, nil, c.headers, &resp)
	if err != nil {
		c.apiError = true
		return fmt.Errorf("making api request: %w", err)
	}

	// validate response structure
	if resp == nil {
		c.apiError = true
		return fmt.Errorf("received nil response from API")
	}

	c.unregisteredCache = make(map[string]bool)
	for _, unreg := range resp.Unregistered {
		c.unregisteredCache[strings.ToUpper(unreg.InfoHash)] = true
	}

	c.log.Debugf("Cached %d unregistered torrents from PTP", len(c.unregisteredCache))
	return nil
}

func (c *PTP) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	c.unregisteredCacheMux.Lock()
	if !c.unregisteredFetched {
		if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
			c.log.Info("-----")
			torrent.APIDividerPrinted = true
		}

		if err := c.fetchUnregisteredTorrents(ctx); err != nil {
			// mark as fetched to prevent retrying on every torrent
			c.unregisteredFetched = true
			c.unregisteredCacheMux.Unlock()
			c.log.Errorf("Failed to fetch unregistered torrents from PTP API: %v", err)
			return nil, false
		}
		c.unregisteredFetched = true
	}
	c.unregisteredCacheMux.Unlock()

	c.unregisteredCacheMux.RLock()
	isUnregistered := c.unregisteredCache[strings.ToUpper(torrent.Hash)]
	c.unregisteredCacheMux.RUnlock()

	if isUnregistered {
		c.log.Tracef("Torrent %s (hash: %s) found in unregistered cache", torrent.Name, torrent.Hash)
	} else {
		c.log.Tracef("Torrent %s (hash: %s) not found in unregistered cache", torrent.Name, torrent.Hash)
	}

	return nil, isUnregistered
}

func (c *PTP) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, c.apiError
}
