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
	cfg     BTNConfig
	http    *http.Client
	headers map[string]string
	log     *logrus.Entry
}

func NewBTN(c BTNConfig) *BTN {
	l := logger.GetLogger("btn-api")
	return &BTN{
		cfg:  c,
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack), l),
		headers: map[string]string{
			"Accept": "application/json",
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

	re := regexp.MustCompile(`https?://[^/]*broadcasthe\.net/torrents\.php\?action=reqlink&id=(\d+)`)
	matches := re.FindStringSubmatch(comment)

	if len(matches) < 2 {
		return "", fmt.Errorf("no torrent ID found in comment: %s", comment)
	}

	return matches[1], nil
}

func (c *BTN) IsUnregistered(torrent *Torrent) (bool, error) {
	if !strings.EqualFold(torrent.TrackerName, "landof.tv") {
		return false, nil
	}

	if torrent.Comment == "" {
		return false, nil
	}

	torrentID, err := c.extractTorrentID(torrent.Comment)
	if err != nil {
		return false, nil
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

	type TorrentsResponse struct {
		Results  string                 `json:"results"`
		Torrents map[string]TorrentInfo `json:"torrents"`
	}

	type JSONRPCResponse struct {
		JsonRPC string           `json:"jsonrpc"`
		Result  TorrentsResponse `json:"result"`
		Error   *struct {
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
		c.log.WithError(err).Error("failed to marshal request body")
		return false, fmt.Errorf("marshal request: %w", err)
	}

	// create request
	req, err := http.NewRequest(http.MethodPost, "https://api.broadcasthe.net", bytes.NewReader(jsonBody))
	if err != nil {
		c.log.WithError(err).Error("failed to create request")
		return false, fmt.Errorf("create request: %w", err)
	}

	// set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// send request
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.WithError(err).Errorf("failed checking torrent %s (hash: %s)", torrent.Name, torrent.Hash)
		return false, fmt.Errorf("request check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// decode response
	var response JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		c.log.WithError(err).Errorf("failed decoding response for %s (hash: %s)", torrent.Name, torrent.Hash)
		return false, fmt.Errorf("decode response: %w", err)
	}

	// check for RPC error
	if response.Error != nil {
		// check message content for IP authorization
		if strings.Contains(strings.ToLower(response.Error.Message), "ip address needs authorization") {
			c.log.Error("api requires ip authorization - check btn notices")
			return false, fmt.Errorf("ip authorization required")
		}

		// default error case
		c.log.WithError(fmt.Errorf(response.Error.Message)).Errorf("api error (code: %d)", response.Error.Code)
		return false, fmt.Errorf("api error: %s (code: %d)", response.Error.Message, response.Error.Code)
	}

	// check if we got any results
	if response.Result.Results == "0" || len(response.Result.Torrents) == 0 {
		return true, nil
	}

	// compare infohash
	for _, t := range response.Result.Torrents {
		if strings.EqualFold(t.InfoHash, torrent.Hash) {
			c.log.Debugf("found matching torrent: %s", t.ReleaseName)
			return false, nil
		}
	}

	// if we get here, the torrent ID exists but hash doesn't match
	c.log.Debugf("torrent id exists but hash mismatch for: %s", torrent.Name)
	return true, nil
}

func (c *BTN) IsTrackerDown(torrent *Torrent) (bool, error) {
	return false, nil
}
