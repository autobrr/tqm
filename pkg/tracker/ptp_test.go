package tracker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/tqm/pkg/logger"
)

// TestPTPWithMockServer tests PTP with a mock HTTP server
func TestPTP_IsUnregistered_WithMockServer(t *testing.T) {
	// Track API calls
	var apiCalls int
	var mu sync.Mutex

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		apiCalls++
		mu.Unlock()

		// Verify endpoint
		if !strings.Contains(r.URL.Path, "userhistory.php") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Return mock data
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"Total": 2,
			"Page": 1,
			"Pages": 1,
			"Unregistered": [
				{"InfoHash": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
				{"InfoHash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
			]
		}`)
	}))
	defer server.Close()

	// Create a custom http client that rewrites requests to our test server
	transport := &testTransport{
		server: server,
		base:   http.DefaultTransport,
	}
	testClient := &http.Client{Transport: transport}

	// Create PTP instance with test client
	ptp := &PTP{
		cfg:               PTPConfig{User: "test", Key: "test"},
		http:              testClient,
		headers:           map[string]string{"ApiUser": "test", "ApiKey": "test"},
		log:               logger.GetLogger("test"),
		unregisteredCache: make(map[string]bool),
	}

	ctx := context.Background()

	// Test 1: First call fetches from API
	t1 := &Torrent{Hash: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Name: "Test1"}
	err, isUnreg := ptp.IsUnregistered(ctx, t1)
	require.NoError(t, err)
	assert.True(t, isUnreg)
	assert.Equal(t, 1, apiCalls)

	// Test 2: Second call uses cache
	err, isUnreg = ptp.IsUnregistered(ctx, t1)
	require.NoError(t, err)
	assert.True(t, isUnreg)
	assert.Equal(t, 1, apiCalls) // Still 1

	// Test 3: Different hash, still uses cache
	t2 := &Torrent{Hash: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC", Name: "Test2"}
	err, isUnreg = ptp.IsUnregistered(ctx, t2)
	require.NoError(t, err)
	assert.False(t, isUnreg)     // Not in unregistered list
	assert.Equal(t, 1, apiCalls) // Still 1
}

// testTransport redirects requests to test server
type testTransport struct {
	server *httptest.Server
	base   http.RoundTripper
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite URL to point to test server
	req.URL.Scheme = "http"
	req.URL.Host = t.server.Listener.Addr().String()
	return t.base.RoundTrip(req)
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
		{"exact domain", "passthepopcorn.me", true},
		{"with https", "https://passthepopcorn.me", true},
		{"with path", "https://passthepopcorn.me/torrents.php", true},
		{"subdomain", "tracker.passthepopcorn.me", true},
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
