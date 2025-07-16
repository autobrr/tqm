package tracker

import (
	"bytes"
	"context"
	"encoding/json"
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

var torrentIDRegex = regexp.MustCompile(`https?://[^/]*broadcasthe\.net/torrents\.php\?action=reqlink&id=(\d+)`)

type BTNConfig struct {
	Key string `koanf:"api_key"`
}

type BTN struct {
	cfg     BTNConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewBTN(c BTNConfig) *BTN {
	l := logger.GetLogger("btn-api")
	return &BTN{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		log: l,
	}
}

func (c *BTN) Name() string {
	return "BTN"
}

func (c *BTN) Check(host string) bool {
	return strings.EqualFold(host, "landof.tv")
}

// extractTorrentID extracts the torrent ID from the torrent comment field
func (c *BTN) extractTorrentID(comment string) (string, error) {
	if comment == "" {
		return "", fmt.Errorf("empty comment field")
	}

	matches := torrentIDRegex.FindStringSubmatch(comment)

	if len(matches) < 2 {
		return "", fmt.Errorf("no torrent ID found in comment: %s", comment)
	}

	return matches[1], nil
}

func (c *BTN) IsUnregistered(ctx context.Context, torrent *Torrent) (error, bool) {
	type request struct {
		JsonRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
		ID      int    `json:"id"`
	}

	type result struct {
		InfoHash    string `json:"InfoHash"`
		ReleaseName string `json:"ReleaseName"`
	}

	type rpcError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	}

	type response struct {
		JsonRPC string    `json:"jsonrpc"`
		Result  *result   `json:"result,omitempty"`
		Error   *rpcError `json:"error,omitempty"`
		ID      int       `json:"id"`
	}

	if c.log.Logger.IsLevelEnabled(logrus.DebugLevel) {
		c.log.Info("-----")
		torrent.APIDividerPrinted = true
	}

	c.log.Tracef("Querying BTN API for torrent: %s (hash: %s)", torrent.Name, torrent.Hash)

	if !strings.EqualFold(torrent.TrackerName, "landof.tv") {
		return nil, false
	}

	torrentID, err := c.extractTorrentID(torrent.Comment)
	if err != nil {
		return fmt.Errorf("extracting torrent ID: %w", err), false
	}

	payload := &request{
		ID:      1,
		JsonRPC: "2.0",
		Method:  "getTorrentById",
		Params:  [2]string{c.cfg.Key, torrentID},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err), false
	}

	var resp *response
	err = httputils.MakeAPIRequest(ctx, c.http, http.MethodPost, "https://api.broadcasthe.net", bytes.NewReader(body), c.headers, &resp)
	if err != nil {
		return fmt.Errorf("making api request: %w", err), false
	}

	// check for RPC error
	if resp.Error != nil {
		return fmt.Errorf("API error: %s (code: %d)", resp.Error.Message, resp.Error.Code), false
	}

	// check if we got any results
	if resp.Result == nil {
		return nil, true
	}

	// compare infohash
	if strings.EqualFold(resp.Result.InfoHash, torrent.Hash) {
		return nil, false
	}

	// if we get here, the torrent ID exists but hash doesn't match
	c.log.Debugf("Torrent ID exists but hash mismatch for: %s", torrent.Name)
	return nil, true
}

func (c *BTN) IsTrackerDown(_ *Torrent) (error, bool) {
	return nil, false
}
