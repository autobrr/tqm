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

var (
	// defaultUnregisteredStatuses holds the default list if none is provided in config.
	defaultUnregisteredStatuses = []string{
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
	}

	// effectiveUnregisteredStatuses holds the list of statuses to use for checks.
	// It's set by SetEffectiveUnregisteredStatuses based on user config or defaults.
	// All statuses in this list will be lowercased during initialization.
	effectiveUnregisteredStatuses = map[string]struct{}{}

	trackerDownStatuses = []string{
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
	}
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

func (t *Torrent) IsTrackerDown() bool {
	if t.TrackerStatus == "" {
		return false
	}

	status := strings.ToLower(t.TrackerStatus)
	for _, v := range trackerDownStatuses {
		if strings.Contains(status, v) {
			return true
		}
	}

	return false
}

// SetEffectiveUnregisteredStatuses sets the statuses used for IsUnregistered checks.
// If customStatuses is nil or empty, it defaults to defaultUnregisteredStatuses.
// The statuses are stored lowercased for case-insensitive matching.
func SetEffectiveUnregisteredStatuses(customStatuses []string) {
	statusesToUse := defaultUnregisteredStatuses
	if len(customStatuses) > 0 {
		statusesToUse = customStatuses
		logger.GetLogger("config").Debugf("Using custom unregistered statuses from config: %d", len(customStatuses))
	} else {
		logger.GetLogger("config").Debug("Using default unregistered statuses")
	}

	// Reset and populate the effective map, storing lowercased versions.
	effectiveUnregisteredStatuses = make(map[string]struct{}, len(statusesToUse))
	for _, status := range statusesToUse {
		effectiveUnregisteredStatuses[strings.ToLower(strings.TrimSpace(status))] = struct{}{}
	}
}

func (t *Torrent) IsUnregistered() bool {
	if t.IsTrackerDown() {
		return false
	}

	if t.TrackerStatus == "" {
		return false
	}

	// check configured unregistered statuses using exact, case-insensitive match
	statusLower := strings.ToLower(t.TrackerStatus)
	if _, exists := effectiveUnregisteredStatuses[statusLower]; exists {
		return true
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
			return ur
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
