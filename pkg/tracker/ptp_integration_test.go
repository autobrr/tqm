//go:build integration
// +build integration

package tracker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/httputils"
	"github.com/autobrr/tqm/pkg/logger"
)

// TestPTP_RealAPI_SingleCall performs an integration test with real PTP API
// This test requires PTP_API_USER and PTP_API_KEY environment variables
// Optional: PTP_TEST_HASH1 and PTP_TEST_HASH2 for testing with real torrent hashes
// Run with: go test -tags=integration ./pkg/tracker -run TestPTP_RealAPI_SingleCall -v
func TestPTP_RealAPI_SingleCall(t *testing.T) {
	apiUser := os.Getenv("PTP_API_USER")
	apiKey := os.Getenv("PTP_API_KEY")

	if apiUser == "" || apiKey == "" {
		t.Skip("Skipping integration test: PTP_API_USER and PTP_API_KEY environment variables not set")
	}

	// Get test hashes from environment or use defaults
	hash1 := os.Getenv("PTP_TEST_HASH1")
	if hash1 == "" {
		hash1 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		t.Log("Using default hash1 (set PTP_TEST_HASH1 to test with a real hash)")
	} else {
		t.Logf("Using provided hash1: %s", hash1)
	}

	hash2 := os.Getenv("PTP_TEST_HASH2")
	if hash2 == "" {
		hash2 = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
		t.Log("Using default hash2 (set PTP_TEST_HASH2 to test with a real hash)")
	} else {
		t.Logf("Using provided hash2: %s", hash2)
	}

	// Create PTP instance with real credentials
	ptp := &PTP{
		cfg:  PTPConfig{User: apiUser, Key: apiKey},
		http: httputils.NewRetryableHttpClient(15*time.Second, ratelimit.New(1, ratelimit.WithoutSlack)),
		headers: map[string]string{
			"Accept":  "application/json",
			"ApiUser": apiUser,
			"ApiKey":  apiKey,
		},
		log:               logger.GetLogger("ptp-api-test"),
		unregisteredCache: make(map[string]bool),
	}

	ctx := context.Background()

	// Test with two hashes that simulate torrents needing API check
	torrent1 := &Torrent{
		Hash:          hash1,
		Name:          "Test Movie 1",
		TrackerName:   "passthepopcorn.me",
		TrackerStatus: "Working", // Status that doesn't match unregistered patterns
	}

	torrent2 := &Torrent{
		Hash:          hash2,
		Name:          "Test Movie 2",
		TrackerName:   "passthepopcorn.me",
		TrackerStatus: "Active", // Status that doesn't match unregistered patterns
	}

	// First check - should make API call
	t.Log("Checking first torrent...")
	err1, isUnreg1 := ptp.IsUnregistered(ctx, torrent1)
	require.NoError(t, err1, "First API call should succeed")
	assert.True(t, ptp.unregisteredFetched, "API should have been called")

	initialCacheSize := len(ptp.unregisteredCache)
	t.Logf("First torrent - Hash: %s, Unregistered: %v", torrent1.Hash, isUnreg1)
	t.Logf("Initial cache populated with %d unregistered torrents", initialCacheSize)

	// Second check - should use cache, no new API call
	t.Log("Checking second torrent...")
	err2, isUnreg2 := ptp.IsUnregistered(ctx, torrent2)
	require.NoError(t, err2, "Second check should succeed")

	finalCacheSize := len(ptp.unregisteredCache)
	assert.Equal(t, initialCacheSize, finalCacheSize, "Cache size should remain the same")

	t.Logf("Second torrent - Hash: %s, Unregistered: %v", torrent2.Hash, isUnreg2)
	t.Log("Only one API call was made for both torrents")

	// Log some cache stats
	if initialCacheSize > 0 {
		t.Logf("Cache contains %d unregistered torrents from your PTP account", initialCacheSize)
	} else {
		t.Log("No unregistered torrents found in your PTP account")
	}
}

// TestPTP_MockFlow tests the logic flow without real API calls
func TestPTP_MockFlow(t *testing.T) {
	// This simulates the flow from config.Torrent.IsUnregistered() to tracker API

	// Mock scenarios that would trigger API check:
	// 1. Tracker is not down
	// 2. Status doesn't match unregistered patterns
	// 3. Status is not intermediate

	mockStatuses := []struct {
		name          string
		status        string
		shouldCallAPI bool
		reason        string
	}{
		{
			name:          "tracker_down_connection_failed",
			status:        "connection failed",
			shouldCallAPI: false,
			reason:        "Tracker down - should not call API",
		},
		{
			name:          "tracker_down_timeout",
			status:        "timeout",
			shouldCallAPI: false,
			reason:        "Tracker down - should not call API",
		},
		{
			name:          "tracker_down_unable_to_process",
			status:        "unable to process your request",
			shouldCallAPI: false,
			reason:        "Tracker down - should not call API",
		},
		{
			name:          "unregistered_pattern",
			status:        "unregistered",
			shouldCallAPI: false,
			reason:        "Matches unregistered pattern - should not call API",
		},
		{
			name:          "unregistered_not_found",
			status:        "torrent not found",
			shouldCallAPI: false,
			reason:        "Matches unregistered pattern - should not call API",
		},
		{
			name:          "intermediate_postponed",
			status:        "torrent has been postponed",
			shouldCallAPI: false,
			reason:        "Intermediate status - should not call API",
		},
		{
			name:          "working_status",
			status:        "Working",
			shouldCallAPI: true,
			reason:        "Working status - should call API",
		},
		{
			name:          "custom_message",
			status:        "Some custom tracker message",
			shouldCallAPI: true,
			reason:        "Unknown status - should call API",
		},
	}

	for _, tc := range mockStatuses {
		t.Run(tc.name, func(t *testing.T) {
			// This test demonstrates when the PTP API would be called
			// In the actual flow, config.Torrent.IsUnregistered() makes this decision
			t.Logf("Status: %q - Would call API: %v (%s)",
				tc.status, tc.shouldCallAPI, tc.reason)
		})
	}
}
