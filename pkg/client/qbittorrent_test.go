package client

import (
	"testing"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"

	"github.com/autobrr/tqm/pkg/config"
)

func TestQBittorrent_ProcessTrackerStatuses(t *testing.T) {
	tests := []struct {
		name                       string
		trackers                   []qbittorrent.TorrentTracker
		expectedTrackerName        string
		expectedTrackerStatus      string
		expectedAllTrackerStatuses map[string]string
	}{
		{
			name: "multiple_trackers_first_down",
			trackers: []qbittorrent.TorrentTracker{
				{
					Url:     "http://tracker1.com/announce",
					Message: "Connection failed",
				},
				{
					Url:     "http://tracker2.com/announce",
					Message: "Working",
				},
				{
					Url:     "http://tracker3.com/announce",
					Message: "Active",
				},
			},
			expectedTrackerName:   "tracker1.com",
			expectedTrackerStatus: "Connection failed",
			expectedAllTrackerStatuses: map[string]string{
				"http://tracker1.com/announce": "Connection failed",
				"http://tracker2.com/announce": "Working",
				"http://tracker3.com/announce": "Active",
			},
		},
		{
			name: "skip_disabled_trackers",
			trackers: []qbittorrent.TorrentTracker{
				{
					Url:     "[DHT]",
					Message: "DHT active",
				},
				{
					Url:     "[LSD]",
					Message: "LSD active",
				},
				{
					Url:     "[PeX]",
					Message: "PeX active",
				},
				{
					Url:     "http://tracker1.com/announce",
					Message: "Working",
				},
			},
			expectedTrackerName:   "tracker1.com",
			expectedTrackerStatus: "Working",
			expectedAllTrackerStatuses: map[string]string{
				"http://tracker1.com/announce": "Working",
			},
		},
		{
			name: "empty_tracker_messages",
			trackers: []qbittorrent.TorrentTracker{
				{
					Url:     "http://tracker1.com/announce",
					Message: "",
				},
				{
					Url:     "http://tracker2.com/announce",
					Message: "Working",
				},
			},
			expectedTrackerName:   "tracker1.com",
			expectedTrackerStatus: "",
			expectedAllTrackerStatuses: map[string]string{
				"http://tracker2.com/announce": "Working",
			},
		},
		{
			name: "all_trackers_have_status",
			trackers: []qbittorrent.TorrentTracker{
				{
					Url:     "http://tracker1.com/announce",
					Message: "timeout",
				},
				{
					Url:     "http://tracker2.com/announce",
					Message: "connection refused",
				},
				{
					Url:     "http://tracker3.com/announce",
					Message: "bad gateway",
				},
			},
			expectedTrackerName:   "tracker1.com",
			expectedTrackerStatus: "timeout",
			expectedAllTrackerStatuses: map[string]string{
				"http://tracker1.com/announce": "timeout",
				"http://tracker2.com/announce": "connection refused",
				"http://tracker3.com/announce": "bad gateway",
			},
		},
		{
			name:                       "no_trackers",
			trackers:                   []qbittorrent.TorrentTracker{},
			expectedTrackerName:        "",
			expectedTrackerStatus:      "",
			expectedAllTrackerStatuses: map[string]string{},
		},
		{
			name: "tracker_url_with_port",
			trackers: []qbittorrent.TorrentTracker{
				{
					Url:     "http://tracker1.com:8080/announce",
					Message: "Working",
				},
			},
			expectedTrackerName:   "tracker1.com",
			expectedTrackerStatus: "Working",
			expectedAllTrackerStatuses: map[string]string{
				"http://tracker1.com:8080/announce": "Working",
			},
		},
		{
			name: "tracker_url_with_subdomain",
			trackers: []qbittorrent.TorrentTracker{
				{
					Url:     "http://announce.tracker1.com/announce",
					Message: "Working",
				},
			},
			expectedTrackerName:   "tracker1.com",
			expectedTrackerStatus: "Working",
			expectedAllTrackerStatuses: map[string]string{
				"http://announce.tracker1.com/announce": "Working",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the tracker processing logic from GetTorrents
			trackerName := ""
			trackerStatus := ""
			allTrackerStatuses := make(map[string]string)
			firstTrackerSet := false

			for _, tr := range tt.trackers {
				// skip disabled trackers
				if tr.Url == "[DHT]" || tr.Url == "[LSD]" || tr.Url == "[PeX]" {
					continue
				}

				// Store all tracker statuses
				if tr.Message != "" {
					allTrackerStatuses[tr.Url] = tr.Message
				}

				// Keep first tracker for backward compatibility
				if !firstTrackerSet {
					trackerName = config.ParseTrackerDomain(tr.Url)
					trackerStatus = tr.Message
					firstTrackerSet = true
				}
			}

			// Verify results
			assert.Equal(t, tt.expectedTrackerName, trackerName)
			assert.Equal(t, tt.expectedTrackerStatus, trackerStatus)
			assert.Len(t, allTrackerStatuses, len(tt.expectedAllTrackerStatuses))
			for url, status := range tt.expectedAllTrackerStatuses {
				assert.Equal(t, status, allTrackerStatuses[url])
			}
		})
	}
}

func TestParseTrackerDomain(t *testing.T) {
	tests := []struct {
		name           string
		trackerHost    string
		expectedDomain string
	}{
		{
			name:           "simple_url",
			trackerHost:    "http://tracker.com/announce",
			expectedDomain: "tracker.com",
		},
		{
			name:           "url_with_port",
			trackerHost:    "http://tracker.com:8080/announce",
			expectedDomain: "tracker.com",
		},
		{
			name:           "url_with_subdomain",
			trackerHost:    "http://announce.tracker.com/announce",
			expectedDomain: "tracker.com",
		},
		{
			name:           "https_url",
			trackerHost:    "https://secure.tracker.com/announce",
			expectedDomain: "tracker.com",
		},
		{
			name:           "complex_subdomain",
			trackerHost:    "http://announce.sub.tracker.com/announce",
			expectedDomain: "tracker.com",
		},
		{
			name:           "empty_host",
			trackerHost:    "",
			expectedDomain: "",
		},
		{
			name:           "invalid_url",
			trackerHost:    "not-a-url",
			expectedDomain: "", // ParseTrackerDomain returns empty string for invalid URLs
		},
		{
			name:           "ip_address",
			trackerHost:    "http://192.168.1.1:8080/announce",
			expectedDomain: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ParseTrackerDomain(tt.trackerHost)
			assert.Equal(t, tt.expectedDomain, got)
		})
	}
}
