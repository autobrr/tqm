package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/tqm/pkg/logger"
)

// redirectTransport redirects requests to test server
type redirectTransport struct {
	server *httptest.Server
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(req)
}

func TestPTP_IsUnregistered_CaseInsensitive(t *testing.T) {
	ptp := &PTP{
		unregisteredCache: map[string]bool{
			"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA": true,
		},
		unregisteredFetched: true,
		log:                 logger.GetLogger("test"),
	}

	ctx := context.Background()

	tests := []struct {
		name string
		hash string
	}{
		{"lowercase", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"uppercase", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"mixed", "AaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := &Torrent{Hash: tt.hash, Name: "Test"}
			err, isUnreg := ptp.IsUnregistered(ctx, torrent)
			require.NoError(t, err)
			assert.True(t, isUnreg)
		})
	}
}

func TestPTP_Check(t *testing.T) {
	ptp := &PTP{}

	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{"exact domain", ptpDomain, true},
		{"with https", "https://" + ptpDomain, true},
		{"with path", "https://" + ptpDomain + ptpUserHistoryEndpoint, true},
		{"subdomain", "tracker." + ptpDomain, true},
		{"wrong domain", "example.com", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ptp.Check(tt.host)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPTP_IsUnregistered_EmptyCache(t *testing.T) {
	// Create minimal PTP instance with empty cache already fetched
	ptp := &PTP{
		unregisteredCache:   make(map[string]bool),
		unregisteredFetched: true,
		log:                 logger.GetLogger("test"),
	}

	ctx := context.Background()
	torrent := &Torrent{
		Hash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Name: "Test Torrent",
	}

	err, isUnreg := ptp.IsUnregistered(ctx, torrent)
	require.NoError(t, err)
	assert.False(t, isUnreg, "Torrent should not be unregistered when not in cache")
}

func TestPTP_IsTrackerDown_AfterAPIError(t *testing.T) {
	// Test that IsTrackerDown returns true after API error
	ptp := &PTP{
		apiError: true,
		log:      logger.GetLogger("test"),
	}

	err, isDown := ptp.IsTrackerDown(&Torrent{})
	require.NoError(t, err)
	assert.True(t, isDown, "Tracker should be marked as down after API error")
}

func TestPTP_APIError_Handling(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	// Create transport that redirects to test server
	testClient := &http.Client{
		Transport: &redirectTransport{server: server},
	}

	// Create PTP instance
	ptp := &PTP{
		cfg:               PTPConfig{User: "test", Key: "test"},
		http:              testClient,
		headers:           map[string]string{"ApiUser": "test", "ApiKey": "test"},
		log:               logger.GetLogger("test"),
		unregisteredCache: make(map[string]bool),
	}

	ctx := context.Background()
	torrent := &Torrent{
		Hash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Name: "Test",
	}

	// First call should attempt API and fail gracefully
	err, isUnreg := ptp.IsUnregistered(ctx, torrent)
	assert.NoError(t, err, "Should not return error to caller")
	assert.False(t, isUnreg, "Should return false when API fails")
	assert.True(t, ptp.unregisteredFetched, "Should mark as fetched even on error")
	assert.True(t, ptp.apiError, "Should set apiError flag")

	// Verify tracker is marked as down
	_, isDown := ptp.IsTrackerDown(torrent)
	assert.True(t, isDown, "Tracker should be marked as down")

	// Second call should not retry API
	err2, isUnreg2 := ptp.IsUnregistered(ctx, torrent)
	assert.NoError(t, err2)
	assert.False(t, isUnreg2)
}
