package config

import (
	"math"
	"strings"

	"github.com/autobrr/tqm/sliceutils"
	"github.com/autobrr/tqm/tracker"
)

var (
	unregisteredStatuses = []string{
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
		//"truncated", // Tracker is down
		"unknown",
		"unregistered",
		"upgraded",
		"uploaded",
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

	// set by command
	HardlinkedOutsideClient bool `json:"-"`
}

func (t *Torrent) IsUnregistered() bool {
	if t.TrackerStatus == "" || strings.Contains(t.TrackerStatus, "Tracker is down") {
		return false
	}

	// check hardcoded unregistered statuses
	status := strings.ToLower(t.TrackerStatus)
	for _, v := range unregisteredStatuses {
		// unregistered tracker status found?
		if strings.Contains(status, v) {
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

func (t *Torrent) Log(n float64) float64 {
	return math.Log(n)
}
