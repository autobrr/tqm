package client

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	qbit "github.com/autobrr/go-qbittorrent"
	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"

	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/expression"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/sliceutils"
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
	client     *qbit.Client

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
	qbl.Out = io.Discard
	//tc.client = qbittorrent.NewClient(strings.TrimSuffix(*tc.Url, "/"), qbl)
	tc.client = qbit.NewClient(qbit.Config{
		Host:          *tc.Url,
		Username:      tc.User,
		Password:      tc.Password,
		TLSSkipVerify: true,
		BasicUser:     tc.User,
		BasicPass:     tc.Password,
		Log:           nil,
	})

	return &tc, nil
}

/* Interface  */

func (c *QBittorrent) Type() string {
	return c.clientType
}

func (c *QBittorrent) Connect(context.Context) error {
	// login
	if err := c.client.Login(); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// retrieve & validate api version
	//apiVersion, err := c.client.Application.GetAPIVersion()
	apiVersion, err := c.client.GetWebAPIVersion()
	if err != nil {
		return fmt.Errorf("get api version: %w", err)
	}
	//else if stringutils.Atof64(apiVersion[0:3], 0.0) < 2.2 {
	//	return fmt.Errorf("unsupported webapi version: %v", apiVersion)
	//}

	c.log.Debugf("API Version: %v", apiVersion)
	return nil
}

func (c *QBittorrent) LoadLabelPathMap(ctx context.Context) error {
	p, err := c.client.GetAppPreferencesCtx(ctx)
	if err != nil {
		return fmt.Errorf("get app preferences: %w", err)
	}

	cats, err := c.client.GetCategoriesCtx(ctx)
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

func (c *QBittorrent) GetTorrents(ctx context.Context) (map[string]config.Torrent, error) {
	// retrieve torrents from client
	c.log.Tracef("Retrieving torrents...")
	t, err := c.client.GetTorrentsCtx(ctx, qbit.TorrentFilterOptions{IncludeTrackers: true})
	if err != nil {
		return nil, fmt.Errorf("get torrents: %w", err)
	}
	c.log.Tracef("Retrieved %d torrents", len(t))

	// build torrent list
	torrents := make(map[string]config.Torrent)
	for _, t := range t {
		// get additional torrent details
		//td, err := c.client.Torrent.GetProperties(t.Hash)
		td, err := c.client.GetTorrentPropertiesCtx(ctx, t.Hash)
		if err != nil {
			return nil, fmt.Errorf("get torrent properties: %v: %w", t.Hash, err)
		}

		tf, err := c.client.GetFilesInformationCtx(ctx, t.Hash)
		if err != nil {
			return nil, fmt.Errorf("get torrent files: %v: %w", t.Hash, err)
		}

		// parse tracker details
		trackerName := ""
		trackerStatus := ""

		var trackers []qbit.TorrentTracker

		trackers = t.Trackers

		// in qBittorrent v5.1+ we can use includeTrackers to populate trackers, but in older versions we need to fetch trackers per torrent
		if len(t.Trackers) == 0 {
			ts, err := c.client.GetTorrentTrackersCtx(ctx, t.Hash)
			if err != nil {
				return nil, fmt.Errorf("get torrent trackers: %v: %w", t.Hash, err)
			}
			trackers = ts
		}

		for _, tracker := range trackers {
			// skip disabled trackers
			if strings.Contains(tracker.Url, "[DHT]") || strings.Contains(tracker.Url, "[LSD]") ||
				strings.Contains(tracker.Url, "[PeX]") {
				continue
			}

			// use status of first enabled tracker
			trackerName = parseTrackerDomain(tracker.Url)
			trackerStatus = tracker.Message
			break
		}

		// added time
		addedTimeSecs := int64(time.Since(time.Unix(int64(td.AdditionDate), 0)).Seconds())

		seedingTime := time.Duration(td.SeedingTime) * time.Second

		// torrent files
		var files []string
		for _, f := range *tf {
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
			TotalBytes:      t.Size,
			DownloadedBytes: td.TotalDownloaded,
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
			SeedingSeconds: int64(seedingTime.Seconds()),
			SeedingHours:   float32(seedingTime.Seconds()) / 60 / 60,
			SeedingDays:    float32(seedingTime.Seconds()) / 60 / 60 / 24,
			UpLimit:        int64(td.UpLimit),
			Label:          t.Category,
			Seeds:          int64(td.SeedsTotal),
			Peers:          int64(td.PeersTotal),
			IsPrivate:      td.IsPrivate,
			IsPublic:       !td.IsPrivate,
			// free space
			FreeSpaceGB:  c.GetFreeSpace,
			FreeSpaceSet: c.freeSpaceSet,
			// tracker
			TrackerName:   trackerName,
			TrackerStatus: trackerStatus,
			Comment:       td.Comment,
		}

		torrents[t.Hash] = torrent
	}

	return torrents, nil
}

func (c *QBittorrent) RemoveTorrent(ctx context.Context, hash string, deleteData bool) (bool, error) {
	// pause torrent
	if err := c.client.PauseCtx(ctx, []string{hash}); err != nil {
		return false, fmt.Errorf("pause torrent: %v: %w", hash, err)
	}

	time.Sleep(1 * time.Second)

	// resume torrent
	if err := c.client.ResumeCtx(ctx, []string{hash}); err != nil {
		return false, fmt.Errorf("resume torrent: %v: %w", hash, err)
	}

	// sleep before re-announcing torrent
	time.Sleep(2 * time.Second)

	if err := c.client.ReAnnounceTorrentsCtx(ctx, []string{hash}); err != nil {
		return false, fmt.Errorf("re-announce torrent: %v: %w", hash, err)
	}

	// sleep before removing torrent
	time.Sleep(2 * time.Second)

	// remove
	if err := c.client.DeleteTorrentsCtx(ctx, []string{hash}, deleteData); err != nil {
		return false, fmt.Errorf("delete torrent: %v: %w", hash, err)
	}

	return true, nil
}

func (c *QBittorrent) SetTorrentLabel(ctx context.Context, hash string, label string, hardlink bool) error {
	if hardlink {
		// get label path
		lp := c.labelPathMap[label]
		if lp == "" {
			return fmt.Errorf("label path not found for label %v", label)
		}

		// get torrent details
		td, err := c.client.GetTorrentPropertiesCtx(ctx, hash)
		if err != nil {
			return fmt.Errorf("get torrent properties: %w", err)
		}

		if filepath.Clean(td.SavePath) != filepath.Clean(lp) {
			// get torrent files
			tf, err := c.client.GetFilesInformationCtx(ctx, hash)
			if err != nil {
				return fmt.Errorf("get torrent files: %w", err)
			}

			for _, f := range *tf {
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
		if err := c.client.SetAutoManagementCtx(ctx, []string{hash}, false); err != nil {
			return fmt.Errorf("set automatic management: %w", err)
		}
		if err := c.client.SetLocationCtx(ctx, []string{hash}, lp); err != nil {
			return fmt.Errorf("set location: %w", err)
		}
	}

	// set label
	if err := c.client.SetCategoryCtx(ctx, []string{hash}, label); err != nil {
		return fmt.Errorf("set torrent label: %v: %w", label, err)
	}

	// enable autotmm
	if c.EnableAutoTmmAfterRelabel && !hardlink {
		if err := c.client.SetAutoManagementCtx(ctx, []string{hash}, true); err != nil {
			return fmt.Errorf("enable autotmm: %w", err)
		}
	}

	return nil
}

func (c *QBittorrent) SetUploadLimit(ctx context.Context, hash string, limit int64) error {
	err := c.client.SetTorrentUploadLimitCtx(ctx, []string{hash}, limit)
	if err != nil {
		return fmt.Errorf("set upload limit for %s: %w", hash, err)
	}

	c.log.Debugf("Set upload limit for torrent %s to %d KiB/s", hash, limit)
	return nil
}

func (c *QBittorrent) GetCurrentFreeSpace(ctx context.Context, path string) (int64, error) {
	// get current main stats
	data, err := c.client.SyncMainDataCtx(ctx, 0)
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

func (c *QBittorrent) ShouldIgnore(ctx context.Context, t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(ctx, t, c.exp.Ignores)
	if err != nil {
		return true, fmt.Errorf("check ignore expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *QBittorrent) ShouldRemove(ctx context.Context, t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(ctx, t, c.exp.Removes)
	if err != nil {
		return false, fmt.Errorf("check remove expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *QBittorrent) ShouldRemoveWithReason(ctx context.Context, t *config.Torrent) (bool, string, error) {
	match, reason, err := expression.CheckTorrentSingleMatchWithReason(ctx, t, c.exp.Removes)
	if err != nil {
		return false, "", fmt.Errorf("check remove expression: %v: %w", t.Hash, err)
	}

	return match, reason, nil
}

func (c *QBittorrent) ShouldRelabel(ctx context.Context, t *config.Torrent) (string, bool, error) {
	for _, label := range c.exp.Labels {
		// check update
		match, err := expression.CheckTorrentAllMatch(ctx, t, label.Updates)
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

func (c *QBittorrent) CheckTorrentPause(ctx context.Context, t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(ctx, t, c.exp.Pauses)
	if err != nil {
		return false, fmt.Errorf("check pause expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *QBittorrent) PauseTorrents(ctx context.Context, hashes []string) error {
	if err := c.client.PauseCtx(ctx, hashes); err != nil {
		return fmt.Errorf("pause torrents: %v: %w", hashes, err)
	}
	return nil
}

func (c *QBittorrent) ShouldRetag(ctx context.Context, t *config.Torrent) (RetagInfo, error) {
	retagInfo := RetagInfo{
		Add:    make(map[string]struct{}),
		Remove: make(map[string]struct{}),
	}
	var uploadLimitSet = false

	for _, tagRule := range c.exp.Tags {
		// check update
		match, err := expression.CheckTorrentAllMatch(ctx, t, tagRule.Updates)
		if err != nil {
			return RetagInfo{}, fmt.Errorf("check update expression for tag %s on torrent %v: %w", tagRule.Name, t.Hash, err)
		}

		var containTag = sliceutils.StringSliceContains(t.Tags, tagRule.Name, false)
		var tagMode = tagRule.Mode

		if containTag && !match && (tagMode == "remove" || tagMode == "full") {
			retagInfo.Remove[tagRule.Name] = struct{}{}
		}
		if !containTag && match && (tagMode == "add" || tagMode == "full") {
			retagInfo.Add[tagRule.Name] = struct{}{}
		}

		if match && tagRule.UploadKb != nil && !uploadLimitSet {
			limitKiB := int64(*tagRule.UploadKb)
			currentLimitKiB := t.UpLimit / 1024

			if currentLimitKiB != limitKiB {
				retagInfo.UploadKb = &limitKiB
				uploadLimitSet = true
			}
		}
	}

	return retagInfo, nil
}

func (c *QBittorrent) AddTags(ctx context.Context, hash string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.AddTagsCtx(ctx, []string{hash}, strings.Join(tags, ",")); err != nil {
		return fmt.Errorf("add torrent tags: %v: %w", tags, err)
	}

	return nil
}

func (c *QBittorrent) RemoveTags(ctx context.Context, hash string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.RemoveTagsCtx(ctx, []string{hash}, strings.Join(tags, ",")); err != nil {
		return fmt.Errorf("add torrent tags: %v: %w", tags, err)
	}

	return nil
}

func (c *QBittorrent) CreateTags(ctx context.Context, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.CreateTagsCtx(ctx, tags); err != nil {
		return fmt.Errorf("create torrent tags: %v: %w", tags, err)
	}

	return nil
}

func (c *QBittorrent) DeleteTags(ctx context.Context, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	if err := c.client.DeleteTagsCtx(ctx, tags); err != nil {
		return fmt.Errorf("delete torrent tags: %v: %w", tags, err)
	}

	return nil
}
