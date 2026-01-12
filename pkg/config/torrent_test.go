package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTorrent_IsTrackerDown(t *testing.T) {
	tests := []struct {
		name         string
		torrent      Torrent
		expectedDown bool
	}{
		{
			name: "all_trackers_down",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Connection failed",
					"http://tracker2.com/announce": "timeout",
					"http://tracker3.com/announce": "service unavailable",
				},
			},
			expectedDown: true,
		},
		{
			name: "some_trackers_down_some_working",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Connection failed",
					"http://tracker2.com/announce": "Working",
					"http://tracker3.com/announce": "timeout",
				},
			},
			expectedDown: false,
		},
		{
			name: "all_trackers_working",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
					"http://tracker2.com/announce": "Active",
					"http://tracker3.com/announce": "Connected",
				},
			},
			expectedDown: false,
		},
		{
			name: "empty_tracker_statuses",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{},
			},
			expectedDown: false,
		},
		{
			name: "nil_AllTrackerStatuses_with_single_tracker_down",
			torrent: Torrent{
				TrackerStatus: "Connection failed",
			},
			expectedDown: true,
		},
		{
			name: "nil_AllTrackerStatuses_with_single_tracker_working",
			torrent: Torrent{
				TrackerStatus: "Working",
			},
			expectedDown: false,
		},
		{
			name: "mixed_http_errors",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "bad request",
					"http://tracker2.com/announce": "unauthorized",
					"http://tracker3.com/announce": "internal server error",
				},
			},
			expectedDown: true,
		},
		{
			name: "case_insensitive_status_check",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "CONNECTION FAILED",
					"http://tracker2.com/announce": "TIMEOUT",
					"http://tracker3.com/announce": "Service Unavailable",
				},
			},
			expectedDown: true,
		},
		{
			name: "empty_status_messages",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "",
					"http://tracker2.com/announce": "",
					"http://tracker3.com/announce": "timeout",
				},
			},
			expectedDown: false,
		},
		{
			name: "only_empty_status_messages",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "",
					"http://tracker2.com/announce": "",
				},
			},
			expectedDown: false,
		},
		{
			name: "tracker_maintenance",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "tracker is down for maintenance",
					"http://tracker2.com/announce": "maintenance mode",
				},
			},
			expectedDown: true,
		},
		{
			name: "ssl_and_network_errors",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "ssl error occurred",
					"http://tracker2.com/announce": "host not found",
					"http://tracker3.com/announce": "unresolvable",
				},
			},
			expectedDown: true,
		},
		// URL false positive prevention tests
		{
			name: "url_containing_down_keyword_not_false_positive",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Torrent trumped, see https://tracker.example.com/torrents/sun-goes-down-2024",
				},
			},
			expectedDown: false,
		},
		{
			name: "url_containing_forbidden_keyword_not_false_positive",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Check https://tracker.example.com/torrents/forbidden-world-1982",
				},
			},
			expectedDown: false,
		},
		{
			name: "url_containing_offline_keyword_not_false_positive",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "See https://tracker.example.com/offline-backup/files",
				},
			},
			expectedDown: false,
		},
		{
			name: "real_error_with_url_still_detected",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Connection failed - see https://tracker.example.com/status for more info",
				},
			},
			expectedDown: true,
		},
		{
			name: "single_tracker_url_containing_down_keyword_not_false_positive",
			torrent: Torrent{
				TrackerStatus: "Replaced by https://tracker.example.com/torrents/falling-down-1993",
			},
			expectedDown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.torrent.IsTrackerDown()
			assert.Equal(t, tt.expectedDown, got)
		})
	}
}

func TestTorrent_IsIntermediateStatus(t *testing.T) {
	tests := []struct {
		name                 string
		torrent              Torrent
		expectedIntermediate bool
	}{
		{
			name: "torrent_postponed_bhd",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "torrent has been postponed",
				},
			},
			expectedIntermediate: true,
		},
		{
			name: "mixed_statuses_with_intermediate",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
					"http://tracker2.com/announce": "torrent has been postponed",
				},
			},
			expectedIntermediate: true,
		},
		{
			name: "no_intermediate_status",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
					"http://tracker2.com/announce": "Active",
				},
			},
			expectedIntermediate: false,
		},
		{
			name: "case_insensitive_intermediate_check",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "TORRENT HAS BEEN POSTPONED",
				},
			},
			expectedIntermediate: true,
		},
		{
			name: "nil_AllTrackerStatuses_with_intermediate",
			torrent: Torrent{
				TrackerStatus: "torrent has been postponed",
			},
			expectedIntermediate: true,
		},
		{
			name: "nil_AllTrackerStatuses_no_intermediate",
			torrent: Torrent{
				TrackerStatus: "Working",
			},
			expectedIntermediate: false,
		},
		// URL false positive prevention tests
		{
			name: "url_containing_postponed_keyword_not_false_positive",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Check https://tracker.example.com/torrents/game-postponed-2024",
				},
			},
			expectedIntermediate: false,
		},
		{
			name: "real_postponed_with_url_still_detected",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "torrent has been postponed - see https://tracker.example.com/help",
				},
			},
			expectedIntermediate: true,
		},
		{
			name: "single_tracker_url_containing_postponed_keyword_not_false_positive",
			torrent: Torrent{
				TrackerStatus: "See https://tracker.example.com/torrents/postponed-wedding-2024",
			},
			expectedIntermediate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.torrent.IsIntermediateStatus()
			assert.Equal(t, tt.expectedIntermediate, got)
		})
	}
}

func TestTorrent_IsUnregistered(t *testing.T) {
	// Initialize the tracker statuses for tests
	InitializeTrackerStatuses(nil)

	tests := []struct {
		name          string
		torrent       Torrent
		expectedUnreg bool
	}{
		{
			name: "one_tracker_unregistered",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
					"http://tracker2.com/announce": "unregistered torrent",
					"http://tracker3.com/announce": "Active",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "all_trackers_down_safety_mechanism",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Connection failed",
					"http://tracker2.com/announce": "timeout",
					"http://tracker3.com/announce": "service unavailable",
				},
			},
			expectedUnreg: false,
		},
		{
			name: "intermediate_status_prevents_unregistered",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "torrent has been postponed",
					"http://tracker2.com/announce": "unregistered",
				},
			},
			expectedUnreg: false,
		},
		{
			name: "mixed_unregistered_messages",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
					"http://tracker2.com/announce": "torrent not found",
					"http://tracker3.com/announce": "Active",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "torrent_deleted",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "torrent has been deleted",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "torrent_nuked",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
					"http://tracker2.com/announce": "torrent has been nuked",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "season_pack_available",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "season pack available",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "trumped_torrent",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "trumped by better quality",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "nil_AllTrackerStatuses_unregistered",
			torrent: Torrent{
				TrackerName:   "tracker.com",
				TrackerStatus: "unregistered torrent",
			},
			expectedUnreg: true,
		},
		{
			name: "nil_AllTrackerStatuses_working",
			torrent: Torrent{
				TrackerName:   "tracker.com",
				TrackerStatus: "Working",
			},
			expectedUnreg: false,
		},
		{
			name: "case_insensitive_unregistered_check",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "UNREGISTERED TORRENT",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "empty_tracker_status",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "",
				},
			},
			expectedUnreg: false,
		},
		{
			name: "cached_registration_state_unregistered",
			torrent: Torrent{
				RegistrationState: UnregisteredState,
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "Working",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "cached_registration_state_registered",
			torrent: Torrent{
				RegistrationState: RegisteredState,
				AllTrackerStatuses: map[string]string{
					"http://tracker1.com/announce": "unregistered",
				},
			},
			expectedUnreg: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.torrent.IsUnregistered(ctx)
			assert.Equal(t, tt.expectedUnreg, got)
		})
	}
}

func TestTorrent_IsUnregistered_PerTrackerOverrides(t *testing.T) {
	// Test with per-tracker custom unregistered statuses
	perTrackerOverrides := map[string][]string{
		"specialtracker.com": {"custom unregistered message", "special removal reason"},
		"anothertracker.com": {"different error", "unique status"},
	}
	InitializeTrackerStatuses(perTrackerOverrides)

	tests := []struct {
		name          string
		torrent       Torrent
		expectedUnreg bool
	}{
		{
			name: "custom_tracker_unregistered_message",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://specialtracker.com/announce": "custom unregistered message",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "custom_tracker_different_message",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://anothertracker.com/announce": "different error",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "default_unregistered_message_on_custom_tracker",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://specialtracker.com/announce": "unregistered",
				},
			},
			expectedUnreg: false, // Custom tracker doesn't include "unregistered" in its list
		},
		{
			name: "normal_tracker_with_default_messages",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://normaltracker.com/announce": "unregistered",
				},
			},
			expectedUnreg: true,
		},
		{
			name: "mixed_custom_and_default_trackers",
			torrent: Torrent{
				AllTrackerStatuses: map[string]string{
					"http://specialtracker.com/announce": "Working",
					"http://normaltracker.com/announce":  "torrent not found",
					"http://anothertracker.com/announce": "Active",
				},
			},
			expectedUnreg: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.torrent.IsUnregistered(ctx)
			assert.Equal(t, tt.expectedUnreg, got)
		})
	}

	// Reset to default for other tests
	InitializeTrackerStatuses(nil)
}

func TestTorrent_TagsSlice(t *testing.T) {
	tests := []struct {
		name        string
		tags        map[string]struct{}
		wantLength  int
		wantContain []string
	}{
		{
			name:        "empty tags",
			tags:        map[string]struct{}{},
			wantLength:  0,
			wantContain: []string{},
		},
		{
			name:        "single tag",
			tags:        map[string]struct{}{"tag1": {}},
			wantLength:  1,
			wantContain: []string{"tag1"},
		},
		{
			name:        "multiple tags",
			tags:        map[string]struct{}{"tag1": {}, "tag2": {}, "tag3": {}},
			wantLength:  3,
			wantContain: []string{"tag1", "tag2", "tag3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := &Torrent{Tags: tt.tags}
			result := torrent.TagsSlice()

			// Check length
			assert.Len(t, result, tt.wantLength)

			// Check all expected tags are present
			for _, tag := range tt.wantContain {
				assert.Contains(t, result, tag)
			}
		})
	}
}

func TestTorrent_HasAllTags(t *testing.T) {
	tests := []struct {
		name        string
		torrentTags map[string]struct{}
		checkTags   []string
		want        bool
	}{
		{
			name:        "has all tags",
			torrentTags: map[string]struct{}{"tag1": {}, "tag2": {}, "tag3": {}},
			checkTags:   []string{"tag1", "tag2"},
			want:        true,
		},
		{
			name:        "missing one tag",
			torrentTags: map[string]struct{}{"tag1": {}, "tag2": {}},
			checkTags:   []string{"tag1", "tag3"},
			want:        false,
		},
		{
			name:        "empty torrent tags",
			torrentTags: map[string]struct{}{},
			checkTags:   []string{"tag1"},
			want:        false,
		},
		{
			name:        "check empty tags",
			torrentTags: map[string]struct{}{"tag1": {}},
			checkTags:   []string{},
			want:        true,
		},
		{
			name:        "exact match",
			torrentTags: map[string]struct{}{"tag1": {}},
			checkTags:   []string{"tag1"},
			want:        true,
		},
		{
			name:        "case sensitive - different case no match",
			torrentTags: map[string]struct{}{"cross-seed": {}, "keep-temp": {}},
			checkTags:   []string{"Cross-Seed", "Keep-Temp"},
			want:        false,
		},
		{
			name:        "case sensitive - mixed tags",
			torrentTags: map[string]struct{}{"Seeding-B": {}, "tag2": {}},
			checkTags:   []string{"Seeding-B", "tag2"},
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := &Torrent{Tags: tt.torrentTags}
			result := torrent.HasAllTags(tt.checkTags...)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestTorrent_HasAnyTag(t *testing.T) {
	tests := []struct {
		name        string
		torrentTags map[string]struct{}
		checkTags   []string
		want        bool
	}{
		{
			name:        "has one of multiple",
			torrentTags: map[string]struct{}{"tag1": {}, "tag2": {}},
			checkTags:   []string{"tag2", "tag3"},
			want:        true,
		},
		{
			name:        "has none",
			torrentTags: map[string]struct{}{"tag1": {}},
			checkTags:   []string{"tag2", "tag3"},
			want:        false,
		},
		{
			name:        "empty torrent tags",
			torrentTags: map[string]struct{}{},
			checkTags:   []string{"tag1"},
			want:        false,
		},
		{
			name:        "check empty tags",
			torrentTags: map[string]struct{}{"tag1": {}},
			checkTags:   []string{},
			want:        false,
		},
		{
			name:        "has all",
			torrentTags: map[string]struct{}{"tag1": {}, "tag2": {}, "tag3": {}},
			checkTags:   []string{"tag1", "tag2"},
			want:        true,
		},
		{
			name:        "case sensitive - no match with different case",
			torrentTags: map[string]struct{}{"cross-seed": {}, "keep-temp": {}},
			checkTags:   []string{"Cross-Seed", "missing-tag"},
			want:        false,
		},
		{
			name:        "case sensitive - exact match",
			torrentTags: map[string]struct{}{"Seeding-B": {}},
			checkTags:   []string{"Seeding-B"},
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := &Torrent{Tags: tt.torrentTags}
			result := torrent.HasAnyTag(tt.checkTags...)
			assert.Equal(t, tt.want, result)
		})
	}
}
