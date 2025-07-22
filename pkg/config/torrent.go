package config

import (
	"context"
	"math"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/bobesa/go-domain-util/domainutil"
	"github.com/sirupsen/logrus"

	"github.com/autobrr/tqm/pkg/evaluate"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/regex"
	"github.com/autobrr/tqm/pkg/tracker"
)

type TorrentRegistrationState uint8

const (
	NoRegistrationState TorrentRegistrationState = iota
	UnregisteredState
	RegisteredState
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
		//"not found", // this is causing too many false positives
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

	// effectiveUnregisteredStatuses stores per-tracker overrides. Key is lowercased tracker name.
	effectiveUnregisteredStatuses = map[string]map[string]struct{}{}
	// defaultUnregisteredStatusesMap is a pre-processed map of the defaults for faster lookups.
	defaultUnregisteredStatusesMap = map[string]struct{}{}

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
		"your request could not be processed, please try again later",
	}

	// set of statuses that indicate that the torrent is not yet fully uploaded, but not unregistered either
	trackerIntermediateStatuses = []string{
		// BHD - torrent is under moderation
		"torrent has been postponed",
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
	IsPrivate       bool     `json:"IsPrivate"`
	IsPublic        bool     `json:"IsPublic"`
	UpLimit         int64    `json:"UpLimit,omitempty"`

	// set by client on GetCurrentFreeSpace
	FreeSpaceGB  func() float64 `json:"-"`
	FreeSpaceSet bool           `json:"-"`

	// tracker
	TrackerName   string `json:"TrackerName"`
	TrackerStatus string `json:"TrackerStatus"`
	// AllTrackerStatuses stores status messages from all trackers (key: tracker URL, value: status message)
	AllTrackerStatuses map[string]string `json:"AllTrackerStatuses,omitempty"`
	Comment            string            `json:"Comment"`

	RegistrationState TorrentRegistrationState `json:"-"`

	// set by command
	HardlinkedOutsideClient bool `json:"-"`
	APIDividerPrinted       bool `json:"-"`

	regexPattern *regex.Pattern
}

func (t *Torrent) IsTrackerDown() bool {
	// If we have multiple tracker statuses, check if ALL are down
	if len(t.AllTrackerStatuses) > 0 {
		workingTrackers := 0
		downTrackers := 0

		for _, status := range t.AllTrackerStatuses {
			if status == "" {
				workingTrackers++
				continue
			}

			statusLower := strings.ToLower(status)
			isDown := false

			for _, v := range trackerDownStatuses {
				if strings.Contains(statusLower, v) {
					isDown = true
					break
				}
			}

			if isDown {
				downTrackers++
			} else {
				workingTrackers++
			}
		}

		// Only consider tracker down if ALL trackers are down (and we have at least one tracker)
		return downTrackers > 0 && workingTrackers == 0
	}

	// Fallback to single tracker status for backward compatibility
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

func (t *Torrent) IsIntermediateStatus() bool {
	// If we have multiple tracker statuses, check if ANY has intermediate status
	if len(t.AllTrackerStatuses) > 0 {
		for _, status := range t.AllTrackerStatuses {
			if status == "" {
				// Empty status means working tracker, not intermediate
				continue
			}

			statusLower := strings.ToLower(status)
			for _, v := range trackerIntermediateStatuses {
				if strings.Contains(statusLower, v) {
					return true
				}
			}
		}
		return false
	}

	// Fallback to single tracker status for backward compatibility
	if t.TrackerStatus == "" {
		return false
	}

	status := strings.ToLower(t.TrackerStatus)
	for _, v := range trackerIntermediateStatuses {
		if strings.Contains(status, v) {
			return true
		}
	}

	return false
}

// InitializeTrackerStatuses prepares the default status map and processes per-tracker overrides.
// It should be called once after configuration is loaded.
func InitializeTrackerStatuses(perTrackerOverrides map[string][]string) {
	log := logger.GetLogger("cfg")

	// Prepare the default map (lowercase).
	defaultUnregisteredStatusesMap = make(map[string]struct{}, len(defaultUnregisteredStatuses))
	for _, status := range defaultUnregisteredStatuses {
		defaultUnregisteredStatusesMap[strings.ToLower(strings.TrimSpace(status))] = struct{}{}
	}
	log.Debugf("Initialized default unregistered statuses: %d entries", len(defaultUnregisteredStatusesMap))

	// Process per-tracker overrides.
	effectiveUnregisteredStatuses = make(map[string]map[string]struct{}, len(perTrackerOverrides))
	if len(perTrackerOverrides) > 0 {
		log.Debugf("Processing %d per-tracker unregistered status overrides", len(perTrackerOverrides))
		for tracker, statuses := range perTrackerOverrides {
			trackerLower := strings.ToLower(strings.TrimSpace(tracker))
			statusMap := make(map[string]struct{}, len(statuses))
			for _, status := range statuses {
				statusMap[strings.ToLower(strings.TrimSpace(status))] = struct{}{}
			}
			effectiveUnregisteredStatuses[trackerLower] = statusMap
			log.Debugf("Set %d custom unregistered statuses for tracker: %s", len(statusMap), tracker)
		}
	} else {
		log.Debug("No per-tracker unregistered status overrides provided, using defaults for all.")
	}
}

func (t *Torrent) IsUnregistered(ctx context.Context) bool {
	switch t.RegistrationState {
	case NoRegistrationState:
	case UnregisteredState:
		return true
	case RegisteredState:
		return false
	}

	// If we have multiple tracker statuses, check them
	if len(t.AllTrackerStatuses) > 0 {
		if t.IsIntermediateStatus() {
			return false
		}

		if t.IsTrackerDown() {
			t.RegistrationState = RegisteredState
			return false
		}

		// Check if ANY tracker reports unregistered status
		for trackerURL, status := range t.AllTrackerStatuses {
			if status == "" {
				continue
			}

			statusLower := strings.ToLower(status)
			trackerDomain := ParseTrackerDomain(trackerURL)
			trackerLower := strings.ToLower(trackerDomain)

			statusMapToCheck := defaultUnregisteredStatusesMap
			if specificMap, ok := effectiveUnregisteredStatuses[trackerLower]; ok {
				statusMapToCheck = specificMap
			}

			for unregStatus := range statusMapToCheck {
				if strings.Contains(statusLower, unregStatus) {
					// At least one tracker reports unregistered
					t.RegistrationState = UnregisteredState
					return true
				}
			}
		}

		// None of the trackers report unregistered
		t.RegistrationState = RegisteredState
		return false
	}

	// Fallback to single tracker status for backward compatibility
	if t.TrackerStatus == "" {
		t.RegistrationState = RegisteredState
		return false
	}

	if t.IsIntermediateStatus() {
		return false
	}

	if t.IsTrackerDown() {
		t.RegistrationState = RegisteredState
		return false
	}

	// check configured unregistered statuses using exact, case-insensitive match.
	// Use per-tracker list if available, otherwise use defaults.
	statusLower := strings.ToLower(t.TrackerStatus)
	trackerLower := strings.ToLower(t.TrackerName)

	statusMapToCheck := defaultUnregisteredStatusesMap // Default to the global defaults
	if specificMap, ok := effectiveUnregisteredStatuses[trackerLower]; ok {
		statusMapToCheck = specificMap // Override with tracker-specific map if it exists
	}

	for status := range statusMapToCheck {
		if strings.Contains(statusLower, status) {
			return true
		}
	}

	// check tracker api (if available)
	if tr := tracker.Get(t.TrackerName); tr != nil {
		tt := &tracker.Torrent{
			Hash:              t.Hash,
			Name:              t.Name,
			TotalBytes:        t.TotalBytes,
			DownloadedBytes:   t.DownloadedBytes,
			State:             t.State,
			Downloaded:        t.Downloaded,
			Seeding:           t.Seeding,
			TrackerName:       t.TrackerName,
			TrackerStatus:     t.TrackerStatus,
			Comment:           t.Comment,
			APIDividerPrinted: t.APIDividerPrinted,
		}

		trackerName := tr.Name()
		if err, ur := tr.IsUnregistered(ctx, tt); err == nil {
			switch ur {
			case true:
				log.Debugf("%s (hash: %s) confirmed as unregistered by %s API", t.Name, t.Hash, trackerName)
				t.RegistrationState = UnregisteredState
			default:
				log.Debugf("%s (hash: %s) not reported as unregistered by %s API", t.Name, t.Hash, trackerName)
				t.RegistrationState = RegisteredState
			}

			t.APIDividerPrinted = tt.APIDividerPrinted
			return ur
		}
	}

	t.RegistrationState = RegisteredState
	return false
}

func (t *Torrent) HasAllTags(tags ...string) bool {
	for _, v := range tags {
		if !evaluate.StringSliceContains(t.Tags, v, true) {
			return false
		}
	}

	return true
}

func (t *Torrent) HasAnyTag(tags ...string) bool {
	for _, v := range tags {
		if evaluate.StringSliceContains(t.Tags, v, true) {
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
	var compiledPatterns []*regex.Pattern
	for p := range strings.SplitSeq(patternsStr, ",") {
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

// RegexMatchAll checks if the torrent name matches all the provided patterns
func (t *Torrent) RegexMatchAll(patternsStr string) bool {
	// Split the comma-separated string into patterns
	var compiledPatterns []*regex.Pattern
	for p := range strings.SplitSeq(patternsStr, ",") {
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

func ParseTrackerDomain(trackerHost string) string {
	// return empty host
	if trackerHost == "" {
		return trackerHost
	}

	// parse url components
	u, err := url.Parse(trackerHost)
	if err != nil {
		logrus.WithError(err).Warnf("Failed parsing tracker host: %q", trackerHost)
		return trackerHost
	}

	// parse host
	host := u.Host
	if strings.Contains(host, ":") {
		// remove port
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}

	// remove subdomain
	if domain := domainutil.Domain(host); domain != "" {
		return domain
	}

	return host
}
