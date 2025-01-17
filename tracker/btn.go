package tracker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/autobrr/tqm/httputils"
	"github.com/autobrr/tqm/logger"
	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"
)

type BTNConfig struct {
	Key string `koanf:"api_key"`
}

type BTN struct {
	cfg  BTNConfig
	http *http.Client
	log  *logrus.Entry
}

func NewBTN(c BTNConfig) *BTN {
	l := logger.GetLogger("btn-api")
	return &BTN{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack), l),
		log:  l,
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

	re := regexp.MustCompile(`https?://[^/]*broadcasthe\.net/torrents\.php\?action=reqlink&id=(\d+)`)
	matches := re.FindStringSubmatch(comment)

	if len(matches) < 2 {
		return "", fmt.Errorf("no torrent ID found in comment: %s", comment)
	}

	return matches[1], nil
}

func (c *BTN) IsUnregistered(torrent *Torrent) (error, bool) {
	if !strings.EqualFold(torrent.TrackerName, "landof.tv") || torrent.Comment == "" {
		return nil, false
	}

	torrentID, err := c.extractTorrentID(torrent.Comment)
	if err != nil {
		return nil, false
	}

	type JSONRPCRequest struct {
		JsonRPC string        `json:"jsonrpc"`
		Method  string        `json:"method"`
		Params  []interface{} `json:"params"`
		ID      int           `json:"id"`
	}

	type TorrentInfo struct {
		InfoHash    string `json:"InfoHash"`
		ReleaseName string `json:"ReleaseName"`
	}

	type JSONRPCResponse struct {
		JsonRPC string `json:"jsonrpc"`
		Result  struct {
			Results  string                 `json:"results"`
			Torrents map[string]TorrentInfo `json:"torrents"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
		ID int `json:"id"`
	}

	// prepare request
	reqBody := JSONRPCRequest{
		JsonRPC: "2.0",
		Method:  "getTorrentsSearch",
		Params:  []interface{}{c.cfg.Key, map[string]interface{}{"id": torrentID}, 1},
		ID:      1,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		c.log.WithError(err).Error("Failed to marshal request body")
		return fmt.Errorf("btn: marshal request: %w", err), false
	}

	// create request
	req, err := http.NewRequest(http.MethodPost, "https://api.broadcasthe.net", bytes.NewReader(jsonBody))
	if err != nil {
		c.log.WithError(err).Error("Failed to create request")
		return fmt.Errorf("btn: create request: %w", err), false
	}

	// set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// send request
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.WithError(err).Errorf("Failed checking torrent %s (hash: %s)", torrent.Name, torrent.Hash)
		return fmt.Errorf("btn: request check: %w", err), false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("btn: unexpected status code: %d", resp.StatusCode), false
	}

	// decode response
	var response JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		c.log.WithError(err).Errorf("Failed decoding response for %s (hash: %s)",
			torrent.Name, torrent.Hash)
		return fmt.Errorf("btn: decode response: %w", err), false
	}

	// check for RPC error
	if response.Error != nil {
		// check message content for IP authorization
		if strings.Contains(strings.ToLower(response.Error.Message), "ip address needs authorization") {
			c.log.Errorf("BTN API requires IP authorization. Please check your notices on BTN")
			return fmt.Errorf("btn: IP authorization required - check BTN notices"), false
		}

		// default error case
		c.log.WithError(fmt.Errorf("%s", response.Error.Message)).Errorf("API error (code: %d)", response.Error.Code)
		return fmt.Errorf("btn: api error: %s (code: %d)", response.Error.Message, response.Error.Code), false
	}

	// check if we got any results
	if response.Result.Results == "0" || len(response.Result.Torrents) == 0 {
		return nil, true
	}

	// compare infohash
	for _, t := range response.Result.Torrents {
		if strings.EqualFold(t.InfoHash, torrent.Hash) {
			c.log.Debugf("Found matching torrent: %s", t.ReleaseName)
			return nil, false
		}
	}

	// if we get here, the torrent ID exists but hash doesn't match
	c.log.Debugf("Torrent ID exists but hash mismatch for: %s", torrent.Name)
	return nil, true
}

func (c *BTN) IsTrackerDown(torrent *Torrent) (error, bool) {
	return nil, false
}
