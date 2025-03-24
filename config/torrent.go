package config

import (
	"math"
	"os"
	"strings"

	"github.com/autobrr/tqm/logger"
	"github.com/autobrr/tqm/regex"
	"github.com/autobrr/tqm/sliceutils"
	"github.com/autobrr/tqm/tracker"
)

// TrackerErrorConfig represents configuration for tracker error status messages
type TrackerErrorConfig struct {
	// List of status patterns indicating a torrent is unregistered
	UnregisteredStatuses []string `yaml:"unregistered_statuses"`
	// List of status patterns indicating a tracker is down
	TrackerDownStatuses []string `yaml:"tracker_down_statuses"`
	// Whether to ignore errors for specific trackers
	IgnoredTrackers []string `yaml:"ignored_trackers"`
	// Map of tracker hosts to lists of ignored status patterns
	TrackerIgnoredStatuses map[string][]string `yaml:"tracker_ignored_statuses"`
}

// DefaultTrackerErrorConfig provides the default configuration
var DefaultTrackerErrorConfig = TrackerErrorConfig{
	UnregisteredStatuses: []string{
		"complete season uploaded",
		"dead",
		"dupe",
		"i'm sorry dave, i can't do that", // RFM specific
		"infohash not found",
		"internal available",
		"not exist",
		"not found",
		"not registered",
		"nuked",
		"pack is available",
		"packs are available",
		"problem with description",
		"problem with file",
		"problem with pack",
		"retitled",
		"season pack",
		"specifically banned",
		"torrent does not exist",
		"torrent existiert nicht",
		"torrent has been deleted",
		"torrent has been nuked",
		"torrent is not authorized for use on this tracker",
		"torrent is not found",
		"torrent nicht gefunden",
		"tracker nicht registriert",
		"torrent not found",
		"trump",
		"unknown",
		"unregistered",
		"upgraded",
		"uploaded",
	},
	TrackerDownStatuses: []string{
		// libtorrent HTTP status messages
		// https://github.com/arvidn/libtorrent/blob/RC_2_0/src/error_code.cpp#L320-L339
		// https://github.com/arvidn/libtorrent/blob/RC_1_2/src/error_code.cpp#L298-L317
		"continue",              // 100 - server still processing
		"multiple choices",      // 300 - could indicate load balancer issues
		"not modified",          // 304 - could be caching issues
		"bad request",           // 400
		"unauthorized",          // 401
		"forbidden",             // 403
		"internal server error", // 500
		"not implemented",       // 501
		"bad gateway",           // 502
		"service unavailable",   // 503
		"moved permanently",     // 301
		"moved temporarily",     // 302
		"(unknown http error)",

		// tracker/network errors
		"down",
		"maintenance",
		"tracker is down",
		"tracker unavailable",
		"truncated",
		"unreachable",
		"not working",
		"not responding",
		"timeout",
		"refused",
		"no connection",
		"cannot connect",
		"connection failed",
		"ssl error",
		"no data",
		"timed out",
		"temporarily disabled",
		"unresolvable",
		"host not found",
		"offline",
	},
	TrackerIgnoredStatuses: map[string][]string{
		//"torrentleech.org": {"not found"},
	},
}

var (
	// These global variables are kept for backward compatibility
	unregisteredStatuses = DefaultTrackerErrorConfig.UnregisteredStatuses
	trackerDownStatuses  = DefaultTrackerErrorConfig.TrackerDownStatuses

	trackerLog = logger.GetLogger("tracker")
)

type Torrent struct {
	// torrent
	Hash            string   `json:"Hash"`
	Name            string   `json:"Name"`
	Path            string   `json:"Path"`
	TotalBytes      int64    `json:"TotalBytes"`
	DownloadedBytes int64    `json:"DownloadedBytes"`
	State           string   `json:"State"`
	Files           []string `json:"Files"`
	Tags            []string `json:"Tags"`
	Downloaded      bool     `json:"Downloaded"`
	Seeding         bool     `json:"Seeding"`
	Ratio           float32  `json:"Ratio"`
	AddedSeconds    int64    `json:"AddedSeconds"`
	AddedHours      float32  `json:"AddedHours"`
	AddedDays       float32  `json:"AddedDays"`
	SeedingSeconds  int64    `json:"SeedingSeconds"`
	SeedingHours    float32  `json:"SeedingHours"`
	SeedingDays     float32  `json:"SeedingDays"`
	Label           string   `json:"Label"`
	Seeds           int64    `json:"Seeds"`
	Peers           int64    `json:"Peers"`

	// set by client on GetCurrentFreeSpace
	FreeSpaceGB  func() float64 `json:"-"`
	FreeSpaceSet bool           `json:"-"`

	// tracker
	TrackerName   string `json:"TrackerName"`
	TrackerStatus string `json:"TrackerStatus"`
	Comment       string `json:"Comment"`

	// set by command
	HardlinkedOutsideClient bool `json:"-"`

	regexPattern *regex.Pattern
}

// IsTrackerDown checks if tracker status indicates the tracker is down
func (t *Torrent) IsTrackerDown() bool {
	if t.TrackerStatus == "" {
		return false
	}

	// Get the global tracker error config
	trackerErrorConfig := DefaultTrackerErrorConfig
	if Config != nil && Config.TrackerErrorConfig != nil {
		trackerErrorConfig = *Config.TrackerErrorConfig
	}

	// Check if this tracker should ignore specific status messages
	if trackerErrorConfig.TrackerIgnoredStatuses != nil {
		if ignoredStatuses, ok := trackerErrorConfig.TrackerIgnoredStatuses[t.TrackerName]; ok {
			status := strings.ToLower(t.TrackerStatus)
			for _, v := range ignoredStatuses {
				if strings.Contains(status, strings.ToLower(v)) {
					trackerLog.Debugf("[%s] Ignoring status %q for tracker %s (matched ignored pattern %q)",
						t.Hash[:8], t.TrackerStatus, t.TrackerName, v)
					return false
				}
			}
		}
	}

	// Check if this tracker is in the ignored trackers list
	if sliceutils.StringSliceContains(trackerErrorConfig.IgnoredTrackers, t.TrackerName, true) {
		trackerLog.Debugf("[%s] Ignoring all tracker status checks for tracker %s (in ignored_trackers list)",
			t.Hash[:8], t.TrackerName)
		return false
	}

	// Check if the status matches any tracker down patterns
	downStatuses := trackerErrorConfig.TrackerDownStatuses
	if len(downStatuses) == 0 {
		downStatuses = trackerDownStatuses // Fall back to the global var for backward compatibility
	}

	status := strings.ToLower(t.TrackerStatus)
	for _, v := range downStatuses {
		if strings.Contains(status, v) {
			trackerLog.Debugf("[%s] Tracker %s appears to be down: status %q matched pattern %q",
				t.Hash[:8], t.TrackerName, t.TrackerStatus, v)
			return true
		}
	}

	return false
}

// IsUnregistered checks if tracker status indicates the torrent is unregistered
func (t *Torrent) IsUnregistered() bool {
	if t.IsTrackerDown() {
		trackerLog.Debugf("[%s] Skipping unregistered check for %s because tracker appears to be down",
			t.Hash[:8], t.TrackerName)
		return false
	}

	if t.TrackerStatus == "" {
		return false
	}

	// Get the global tracker error config
	trackerErrorConfig := DefaultTrackerErrorConfig
	if Config != nil && Config.TrackerErrorConfig != nil {
		trackerErrorConfig = *Config.TrackerErrorConfig
	}

	// Check if this tracker is in the ignored trackers list
	if sliceutils.StringSliceContains(trackerErrorConfig.IgnoredTrackers, t.TrackerName, true) {
		trackerLog.Debugf("[%s] Ignoring all tracker status checks for tracker %s (in ignored_trackers list)",
			t.Hash[:8], t.TrackerName)
		return false
	}

	// Check if this tracker should ignore specific status messages
	if trackerErrorConfig.TrackerIgnoredStatuses != nil {
		if ignoredStatuses, ok := trackerErrorConfig.TrackerIgnoredStatuses[t.TrackerName]; ok {
			status := strings.ToLower(t.TrackerStatus)
			for _, v := range ignoredStatuses {
				if strings.Contains(status, strings.ToLower(v)) {
					trackerLog.Debugf("[%s] Ignoring status %q for tracker %s (matched ignored pattern %q)",
						t.Hash[:8], t.TrackerStatus, t.TrackerName, v)
					return false
				}
			}
		}
	}

	// check unregistered statuses from config
	unregStatuses := trackerErrorConfig.UnregisteredStatuses
	if len(unregStatuses) == 0 {
		unregStatuses = unregisteredStatuses // Fall back to the global var for backward compatibility
	}

	// check hardcoded unregistered statuses
	status := strings.ToLower(t.TrackerStatus)
	for _, v := range unregStatuses {
		// unregistered tracker status found?
		if strings.Contains(status, v) {
			trackerLog.Debugf("[%s] Torrent appears to be unregistered on %s: status %q matched pattern %q",
				t.Hash[:8], t.TrackerName, t.TrackerStatus, v)
			return true
		}
	}

	// check tracker api (if available)
	if tr := tracker.Get(t.TrackerName); tr != nil {
		tt := &tracker.Torrent{
			Hash:            t.Hash,
			Name:            t.Name,
			TotalBytes:      t.TotalBytes,
			DownloadedBytes: t.DownloadedBytes,
			State:           t.State,
			Downloaded:      t.Downloaded,
			Seeding:         t.Seeding,
			TrackerName:     t.TrackerName,
			TrackerStatus:   t.State,
			Comment:         t.Comment,
		}

		if err, ur := tr.IsUnregistered(tt); err == nil {
			if ur {
				trackerLog.Debugf("[%s] Torrent confirmed unregistered via %s API",
					t.Hash[:8], t.TrackerName)
			}
			return ur
		} else {
			trackerLog.Debugf("[%s] Error checking unregistered status via %s API: %v",
				t.Hash[:8], t.TrackerName, err)
		}
	}

	return false
}

func (t *Torrent) HasAllTags(tags ...string) bool {
	for _, v := range tags {
		if !sliceutils.StringSliceContains(t.Tags, v, true) {
			return false
		}
	}

	return true
}

func (t *Torrent) HasAnyTag(tags ...string) bool {
	for _, v := range tags {
		if sliceutils.StringSliceContains(t.Tags, v, true) {
			return true
		}
	}

	return false
}

func (t *Torrent) HasMissingFiles() bool {
	if !t.Downloaded {
		return false
	}

	log := logger.GetLogger("torrent")

	for _, f := range t.Files {
		if f == "" {
			log.Tracef("Skipping empty path for torrent: %s", t.Name)
			continue
		}

		if _, err := os.Stat(f); err != nil {
			if os.IsNotExist(err) {
				return true
			}
			log.Warnf("error checking file '%s' for torrent '%s': %v", f, t.Name, err)
			continue
		}
	}

	return false
}

func (t *Torrent) Log(n float64) float64 {
	return math.Log(n)
}

// RegexMatch delegates to the regex checker
func (t *Torrent) RegexMatch(pattern string) bool {
	// Compile pattern if needed
	if t.regexPattern == nil || t.regexPattern.Expression.String() != pattern {
		compiled, err := regex.Compile(pattern)
		if err != nil {
			return false
		}
		t.regexPattern = compiled
	}

	// Check pattern
	match, err := regex.Check(t.Name, t.regexPattern)
	if err != nil {
		return false
	}

	return match
}

// RegexMatchAny checks if the torrent name matches any of the provided patterns
func (t *Torrent) RegexMatchAny(patternsStr string) bool {
	// Split the comma-separated string into patterns
	patterns := strings.Split(patternsStr, ",")

	var compiledPatterns []*regex.Pattern
	for _, p := range patterns {
		// Trim any whitespace
		p = strings.TrimSpace(p)
		compiled, err := regex.Compile(p)
		if err != nil {
			continue
		}
		compiledPatterns = append(compiledPatterns, compiled)
	}

	match, err := regex.CheckAny(t.Name, compiledPatterns)
	if err != nil {
		return false
	}
	return match
}

// RegexMatchAll checks if the torrent name matches all of the provided patterns
func (t *Torrent) RegexMatchAll(patternsStr string) bool {
	// Split the comma-separated string into patterns
	patterns := strings.Split(patternsStr, ",")

	var compiledPatterns []*regex.Pattern
	for _, p := range patterns {
		// Trim any whitespace
		p = strings.TrimSpace(p)
		compiled, err := regex.Compile(p)
		if err != nil {
			return false
		}
		compiledPatterns = append(compiledPatterns, compiled)
	}

	match, err := regex.CheckAll(t.Name, compiledPatterns)
	if err != nil {
		return false
	}
	return match
}
