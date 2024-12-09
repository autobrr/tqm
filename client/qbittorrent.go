package client

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autobrr/tqm/config"
	"github.com/autobrr/tqm/expression"
	"github.com/autobrr/tqm/logger"
	"github.com/autobrr/tqm/sliceutils"
	"github.com/autobrr/tqm/stringutils"

	"github.com/dustin/go-humanize"
	qbittorrent "github.com/l3uddz/go-qbt"
	"github.com/sirupsen/logrus"
)

/* Struct */

type QBittorrent struct {
	Url                       *string `validate:"required"`
	User                      string
	Password                  string
	EnableAutoTmmAfterRelabel bool

	// internal
	log        *logrus.Entry
	clientType string
	client     *qbittorrent.Client

	// need to be loaded by LoadLabelPathMap
	labelPathMap map[string]string

	// set by cmd handler
	freeSpaceGB  float64
	freeSpaceSet bool

	// internal compiled filters
	exp *expression.Expressions
}

/* Initializer */

func NewQBittorrent(name string, exp *expression.Expressions) (TagInterface, error) {
	tc := QBittorrent{
		log:        logger.GetLogger(name),
		clientType: "qBittorrent",
		exp:        exp,
	}

	// load config
	if err := config.K.Unmarshal(fmt.Sprintf("clients%s%s", config.Delimiter, name), &tc); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// validate config
	if errs := config.ValidateStruct(tc); errs != nil {
		return nil, fmt.Errorf("validate config: %v", errs)
	}

	// init client
	qbl := logrus.New()
	qbl.Out = ioutil.Discard
	tc.client = qbittorrent.NewClient(strings.TrimSuffix(*tc.Url, "/"), qbl)

	return &tc, nil
}

/* Interface  */

func (c *QBittorrent) Type() string {
	return c.clientType
}

func (c *QBittorrent) Connect() error {
	// login
	if err := c.client.Login(c.User, c.Password); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// retrieve & validate api version
	apiVersion, err := c.client.Application.GetAPIVersion()
	if err != nil {
		return fmt.Errorf("get api version: %w", err)
	} else if stringutils.Atof64(apiVersion[0:3], 0.0) < 2.2 {
		return fmt.Errorf("unsupported webapi version: %v", apiVersion)
	}

	c.log.Debugf("API Version: %v", apiVersion)
	return nil
}

func (c *QBittorrent) LoadLabelPathMap() error {
	p, err := c.client.Application.GetAppPreferences()
	if err != nil {
		return fmt.Errorf("get app preferences: %w", err)
	}

	cats, err := c.client.Torrent.GetCategories()
	if err != nil {
		return fmt.Errorf("get categories: %w", err)
	}

	c.labelPathMap = make(map[string]string)
	for _, cat := range cats {
		if cat.SavePath == "" {
			c.labelPathMap[cat.Name] = filepath.Join(p.SavePath, cat.Name)
			continue
		}

		if filepath.IsAbs(cat.SavePath) {
			c.labelPathMap[cat.Name] = cat.SavePath
			continue
		}

		c.labelPathMap[cat.Name] = filepath.Join(p.SavePath, cat.SavePath)
	}

	return nil
}

func (c *QBittorrent) LabelPathMap() map[string]string {
	return c.labelPathMap
}

func (c *QBittorrent) GetTorrents() (map[string]config.Torrent, error) {
	// retrieve torrents from client
	c.log.Tracef("Retrieving torrents...")
	t, err := c.client.Torrent.GetList(nil)
	if err != nil {
		return nil, fmt.Errorf("get torrents: %w", err)
	}
	c.log.Tracef("Retrieved %d torrents", len(t))

	// build torrent list
	torrents := make(map[string]config.Torrent)
	for _, t := range t {
		t := t

		// get additional torrent details
		td, err := c.client.Torrent.GetProperties(t.Hash)
		if err != nil {
			return nil, fmt.Errorf("get torrent properties: %v: %w", t.Hash, err)
		}

		ts, err := c.client.Torrent.GetTrackers(t.Hash)
		if err != nil {
			return nil, fmt.Errorf("get torrent trackers: %v: %w", t.Hash, err)
		}

		tf, err := c.client.Torrent.GetContents(t.Hash)
		if err != nil {
			return nil, fmt.Errorf("get torrent files: %v: %w", t.Hash, err)
		}

		// parse tracker details
		trackerName := ""
		trackerStatus := ""

		for _, tracker := range ts {
			// skip disabled trackers
			if strings.Contains(tracker.URL, "[DHT]") || strings.Contains(tracker.URL, "[LSD]") ||
				strings.Contains(tracker.URL, "[PeX]") {
				continue
			}

			// use status of first enabled tracker
			trackerName = parseTrackerDomain(tracker.URL)
			trackerStatus = tracker.Message
			break
		}

		// added time
		addedTimeSecs := int64(time.Since(td.AdditionDate).Seconds())

		// torrent files
		var files []string
		for _, f := range tf {
			files = append(files, filepath.Join(td.SavePath, f.Name))
		}

		// create torrent
		var tags []string
		if t.Tags == "" {
			tags = []string{}
		} else {
			tags = strings.Split(t.Tags, ", ")
		}
		torrent := config.Torrent{
			Hash:            t.Hash,
			Name:            t.Name,
			Path:            td.SavePath,
			TotalBytes:      int64(t.Size),
			DownloadedBytes: int64(td.TotalDownloaded),
			State:           string(t.State),
			Files:           files,
			Tags:            tags,
			Downloaded: !sliceutils.StringSliceContains([]string{
				"downloading",
				"stalledDL",
				"queuedDL",
				"pausedDL",
				"checkingDL",
			}, string(t.State), true),
			Seeding: sliceutils.StringSliceContains([]string{
				"uploading",
				"stalledUP",
			}, string(t.State), true),
			Ratio:          float32(td.ShareRatio),
			AddedSeconds:   addedTimeSecs,
			AddedHours:     float32(addedTimeSecs) / 60 / 60,
			AddedDays:      float32(addedTimeSecs) / 60 / 60 / 24,
			SeedingSeconds: int64(td.SeedingTime.Seconds()),
			SeedingHours:   float32(td.SeedingTime.Seconds()) / 60 / 60,
			SeedingDays:    float32(td.SeedingTime.Seconds()) / 60 / 60 / 24,
			Label:          t.Category,
			Seeds:          int64(td.SeedsTotal),
			Peers:          int64(td.PeersTotal),
			// free space
			FreeSpaceGB:  c.GetFreeSpace,
			FreeSpaceSet: c.freeSpaceSet,
			// tracker
			TrackerName:   trackerName,
			TrackerStatus: trackerStatus,
		}

		torrents[t.Hash] = torrent
	}

	return torrents, nil
}

func (c *QBittorrent) RemoveTorrent(hash string, deleteData bool) (bool, error) {
	// pause torrent
	if err := c.client.Torrent.StopTorrents([]string{hash}); err != nil {
		return false, fmt.Errorf("pause torrent: %v: %w", hash, err)
	}

	time.Sleep(1 * time.Second)

	// resume torrent
	if err := c.client.Torrent.ResumeTorrents([]string{hash}); err != nil {
		return false, fmt.Errorf("resume torrent: %v: %w", hash, err)
	}

	// sleep before re-announcing torrent
	time.Sleep(2 * time.Second)

	if err := c.client.Torrent.ReannounceTorrents([]string{hash}); err != nil {
		return false, fmt.Errorf("re-announce torrent: %v: %w", hash, err)
	}

	// sleep before removing torrent
	time.Sleep(2 * time.Second)

	// remove
	if err := c.client.Torrent.DeleteTorrents([]string{hash}, deleteData); err != nil {
		return false, fmt.Errorf("delete torrent: %v: %w", hash, err)
	}

	return true, nil
}

func (c *QBittorrent) SetTorrentLabel(hash string, label string, hardlink bool) error {
	if hardlink {
		// get label path
		lp := c.labelPathMap[label]
		if lp == "" {
			return fmt.Errorf("label path not found for label %v", label)
		}

		// get torrent details
		td, err := c.client.Torrent.GetProperties(hash)
		if err != nil {
			return fmt.Errorf("get torrent properties: %w", err)
		}

		if filepath.Clean(td.SavePath) != filepath.Clean(lp) {
			// get torrent files
			tf, err := c.client.Torrent.GetContents(hash)
			if err != nil {
				return fmt.Errorf("get torrent files: %w", err)
			}

			for _, f := range tf {
				source := filepath.Join(td.SavePath, f.Name)
				target := filepath.Join(lp, f.Name)
				if _, err := os.Stat(source); err != nil {
					return fmt.Errorf("stat file '%v': %w", target, err)
				}

				// create target directory
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					return fmt.Errorf("create target directory: %w", err)
				}

				// link
				if err := os.Link(source, target); err != nil {
					return fmt.Errorf("create hardlink for '%v': %w", f.Name, err)
				}
			}
		}

		// if just setting category and letting autotmm move
		// qbit force moves the files, overwriting existing files
		// manually settings location, and then setting category works
		// and causes qbit to recheck instead of move
		if err := c.client.Torrent.SetAutomaticManagement([]string{hash}, false); err != nil {
			return fmt.Errorf("set automatic management: %w", err)
		}
		if err := c.client.Torrent.SetLocations([]string{hash}, lp); err != nil {
			return fmt.Errorf("set location: %w", err)
		}
	}

	// set label
	if err := c.client.Torrent.SetCategories([]string{hash}, label); err != nil {
		return fmt.Errorf("set torrent label: %v: %w", label, err)
	}

	// enable autotmm
	if c.EnableAutoTmmAfterRelabel && !hardlink {
		if err := c.client.Torrent.SetAutomaticManagement([]string{hash}, true); err != nil {
			return fmt.Errorf("enable autotmm: %w", err)
		}
	}

	return nil
}

func (c *QBittorrent) GetCurrentFreeSpace(path string) (int64, error) {
	// get current main stats
	data, err := c.client.Sync.GetMainData(0)
	if err != nil {
		return 0, fmt.Errorf("get main data: %w", err)
	}

	// set internal free size
	c.freeSpaceGB = float64(data.ServerState.FreeSpaceOnDisk) / humanize.GiByte
	c.freeSpaceSet = true

	return int64(data.ServerState.FreeSpaceOnDisk), nil
}

func (c *QBittorrent) AddFreeSpace(bytes int64) {
	c.freeSpaceGB += float64(bytes) / humanize.GiByte
}

func (c *QBittorrent) GetFreeSpace() float64 {
	return c.freeSpaceGB
}

/* Filters */

func (c *QBittorrent) ShouldIgnore(t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(t, c.exp.Ignores)
	if err != nil {
		return true, fmt.Errorf("check ignore expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *QBittorrent) ShouldRemove(t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(t, c.exp.Removes)
	if err != nil {
		return false, fmt.Errorf("check remove expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *QBittorrent) ShouldRelabel(t *config.Torrent) (string, bool, error) {
	for _, label := range c.exp.Labels {
		// check update
		match, err := expression.CheckTorrentAllMatch(t, label.Updates)
		if err != nil {
			return "", false, fmt.Errorf("check update expression: %v: %w", t.Hash, err)
		} else if !match {
			continue
		}

		// we should re-label
		return label.Name, true, nil
	}

	return "", false, nil
}

func (c *QBittorrent) ShouldRetag(t *config.Torrent) (RetagInfo, bool, error) {
	var retagInfo = RetagInfo{}

	for _, tag := range c.exp.Tags {
		// check update
		match, err := expression.CheckTorrentAllMatch(t, tag.Updates)
		if err != nil {
			return RetagInfo{}, false, fmt.Errorf("check update expression: %v: %w", t.Hash, err)
		}

		var containTag = sliceutils.StringSliceContains(t.Tags, tag.Name, false)
		var tagMode = tag.Mode

		if containTag && !match && (tagMode == "remove" || tagMode == "full") {
			// we should remove the tag
			retagInfo.Remove = append(retagInfo.Remove, tag.Name)
		}
		if !containTag && match && (tagMode == "add" || tagMode == "full") {
			// we should add the tag
			retagInfo.Add = append(retagInfo.Add, tag.Name)
		}
	}

	return retagInfo, len(retagInfo.Add) != 0 || len(retagInfo.Remove) != 0, nil
}

func (c *QBittorrent) AddTags(hash string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.Torrent.AddTags([]string{hash}, tags); err != nil {
		return fmt.Errorf("add torrent tags: %v: %w", tags, err)
	}

	return nil
}

func (c *QBittorrent) RemoveTags(hash string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.Torrent.RemoveTags([]string{hash}, tags); err != nil {
		return fmt.Errorf("add torrent tags: %v: %w", tags, err)
	}

	return nil
}

func (c *QBittorrent) CreateTags(tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.Torrent.CreateTags(tags); err != nil {
		return fmt.Errorf("create torrent tags: %v: %w", tags, err)
	}

	return nil
}

func (c *QBittorrent) DeleteTags(tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.Torrent.DeleteTags(tags); err != nil {
		return fmt.Errorf("delete torrent tags: %v: %w", tags, err)
	}

	return nil
}
