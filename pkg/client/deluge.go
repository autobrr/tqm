package client

import (
	"context"
	"fmt"
	"path"
	"time"

	delugeclient "github.com/autobrr/go-deluge"
	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"

	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/expression"
	"github.com/autobrr/tqm/pkg/logger"
)

/* Struct */

type Deluge struct {
	Host     *string `validate:"required"`
	Port     *uint   `validate:"required"`
	Login    *string `validate:"required"`
	Password *string `validate:"required"`
	V2       bool

	// internal
	log        *logrus.Entry
	clientType string
	client     *delugeclient.LabelPlugin
	client1    *delugeclient.Client
	client2    *delugeclient.ClientV2

	// set by cmd handler
	freeSpaceGB  float64
	freeSpaceSet bool

	// internal compiled filters
	exp *expression.Expressions
}

/* Initializer */

func NewDeluge(name string, exp *expression.Expressions) (Interface, error) {
	tc := Deluge{
		log:        logger.GetLogger(name),
		clientType: "Deluge",
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
	settings := delugeclient.Settings{
		Hostname: *tc.Host,
		Port:     *tc.Port,
		Login:    *tc.Login,
		Password: *tc.Password,
	}

	if tc.V2 {
		tc.client2 = delugeclient.NewV2(settings)
	} else {
		tc.client1 = delugeclient.NewV1(settings)
	}

	return &tc, nil
}

/* Interface  */

func (c *Deluge) Type() string {
	return c.clientType
}

func (c *Deluge) Connect(ctx context.Context) error {
	var err error

	// connect to deluge daemon
	c.log.Tracef("Connecting to %s:%d", *c.Host, *c.Port)

	if c.V2 {
		err = c.client2.Connect(ctx)
	} else {
		err = c.client1.Connect(ctx)

	}

	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// retrieve & set common label client
	var lc *delugeclient.LabelPlugin

	if c.V2 {
		lc, err = c.client2.LabelPlugin(ctx)
	} else {
		lc, err = c.client1.LabelPlugin(ctx)
	}

	if err != nil {
		return fmt.Errorf("get label plugin: %w", err)
	}

	// retrieve daemon version
	daemonVersion, err := lc.DaemonVersion(ctx)
	if err != nil {
		return fmt.Errorf("get daemon version: %w", err)
	}
	c.log.Debugf("Daemon Version: %v", daemonVersion)

	c.client = lc
	return nil
}

func (c *Deluge) LoadLabelPathMap(context.Context) error {
	// @TODO: implement
	return nil
}

func (c *Deluge) LabelPathMap() map[string]string {
	return nil
}

func (c *Deluge) GetTorrents(ctx context.Context) (map[string]config.Torrent, error) {
	// retrieve torrents from client
	c.log.Tracef("Retrieving torrents...")
	ts, err := c.client.TorrentsStatus(ctx, delugeclient.StateUnspecified, nil)
	if err != nil {
		return nil, fmt.Errorf("get torrents: %w", err)
	}
	c.log.Tracef("Retrieved %d torrents", len(ts))

	// retrieve torrent labels
	labels, err := c.client.GetTorrentsLabels(delugeclient.StateUnspecified, nil)
	if err != nil {
		return nil, fmt.Errorf("get torrent labels: %w", err)
	}
	c.log.Tracef("Retrieved labels for %d torrents", len(labels))

	// build torrent list
	torrents := make(map[string]config.Torrent)
	for h, t := range ts {
		// build files slice
		var files []string
		for _, f := range t.Files {
			files = append(files, path.Join(t.DownloadLocation, f.Path))
		}

		// get torrent label
		label := ""
		if l, ok := labels[h]; ok {
			label = l
		}

		// create torrent object
		torrent := config.Torrent{
			// torrent
			Hash:            h,
			Name:            t.Name,
			Path:            t.DownloadLocation,
			TotalBytes:      t.TotalSize,
			DownloadedBytes: t.TotalDone,
			State:           t.State,
			Files:           files,
			Downloaded:      t.TotalDone == t.TotalSize,
			Seeding:         t.IsSeed,
			Ratio:           t.Ratio,
			AddedSeconds:    t.ActiveTime,
			AddedHours:      float32(t.ActiveTime) / 60 / 60,
			AddedDays:       float32(t.ActiveTime) / 60 / 60 / 24,
			SeedingSeconds:  t.SeedingTime,
			SeedingHours:    float32(t.SeedingTime) / 60 / 60,
			SeedingDays:     float32(t.SeedingTime) / 60 / 60 / 24,
			Label:           label,
			IsPrivate:       t.Private,
			IsPublic:        !t.Private,
			Seeds:           t.TotalSeeds,
			Peers:           t.TotalPeers,
			// free space
			FreeSpaceGB:  c.GetFreeSpace,
			FreeSpaceSet: c.freeSpaceSet,
			// tracker
			TrackerName:   t.TrackerHost,
			TrackerStatus: t.TrackerStatus,
			// Note: Deluge only uses one tracker at a time, so AllTrackerStatuses is not populated
			AllTrackerStatuses: nil,
		}

		torrents[h] = torrent
	}

	return torrents, nil
}

func (c *Deluge) RemoveTorrent(ctx context.Context, torrent *config.Torrent, deleteData bool) (bool, error) {
	// pause torrent
	if err := c.client.PauseTorrents(ctx, torrent.Hash); err != nil {
		return false, fmt.Errorf("pause torrent: %v: %w", torrent.Hash, err)
	}

	time.Sleep(1 * time.Second)

	// resume torrent
	if err := c.client.ResumeTorrents(ctx, torrent.Hash); err != nil {
		return false, fmt.Errorf("resume torrent: %v: %w", torrent.Hash, err)
	}

	// sleep before re-announcing torrent
	time.Sleep(2 * time.Second)

	// re-announce torrent
	if err := c.client.ForceReannounce(ctx, []string{torrent.Hash}); err != nil {
		return false, fmt.Errorf("re-announce torrent: %v: %w", torrent.Hash, err)
	}

	// sleep before removing torrent
	time.Sleep(2 * time.Second)

	// remove
	if ok, err := c.client.RemoveTorrent(ctx, torrent.Hash, deleteData); err != nil {
		return false, fmt.Errorf("remove torrent: %v: %w", torrent.Hash, err)
	} else if !ok {
		return false, fmt.Errorf("remove torrent: %v", torrent.Hash)
	}

	return true, nil
}

func (c *Deluge) SetTorrentLabel(ctx context.Context, hash string, label string, hardlink bool) error {
	// hardlink behaviour currently not tested for deluge
	if hardlink {
		return fmt.Errorf("hardlink relabeling not supported for deluge (yet)")
	}

	// set label
	if err := c.client.SetTorrentLabel(ctx, hash, label); err != nil {
		return fmt.Errorf("set torrent label: %v: %w", label, err)
	}

	return nil
}

func (c *Deluge) GetCurrentFreeSpace(ctx context.Context, path string) (int64, error) {
	if path == "" {
		return 0, fmt.Errorf("free_space_path is not set for deluge")
	}

	// get free disk space
	space, err := c.client.GetFreeSpace(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("get free disk space: %v: %w", path, err)
	}

	// set internal free size
	c.freeSpaceGB = float64(space) / humanize.GiByte
	c.freeSpaceSet = true

	return space, nil
}

func (c *Deluge) AddFreeSpace(bytes int64) {
	c.freeSpaceGB += float64(bytes) / humanize.GiByte
}

func (c *Deluge) GetFreeSpace() float64 {
	return c.freeSpaceGB
}

/* Filters */

func (c *Deluge) ShouldIgnore(ctx context.Context, t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(ctx, t, c.exp.Ignores)
	if err != nil {
		return true, fmt.Errorf("check ignore expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *Deluge) ShouldRemove(ctx context.Context, t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(ctx, t, c.exp.Removes)
	if err != nil {
		return false, fmt.Errorf("check remove expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *Deluge) ShouldRemoveWithReason(ctx context.Context, t *config.Torrent) (bool, string, error) {
	match, reason, err := expression.CheckTorrentSingleMatchWithReason(ctx, t, c.exp.Removes)
	if err != nil {
		return false, "", fmt.Errorf("check remove expression: %v: %w", t.Hash, err)
	}

	return match, reason, nil
}

func (c *Deluge) ShouldRelabel(ctx context.Context, t *config.Torrent) (string, bool, error) {
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

func (c *Deluge) SetUploadLimit(ctx context.Context, hash string, limit int64) error {
	var uploadSpeed int
	if limit == -1 {
		uploadSpeed = -1
	} else {
		uploadSpeed = int(limit / 1024)
	}

	opts := &delugeclient.Options{
		MaxUploadSpeed: &uploadSpeed,
	}

	var err error
	if c.V2 {
		err = c.client2.SetTorrentOptions(ctx, hash, opts)
	} else {
		err = c.client1.SetTorrentOptions(ctx, hash, opts)
	}

	if err != nil {
		return fmt.Errorf("set torrent options for %s: %w", hash, err)
	}

	c.log.Debugf("Set upload limit for torrent %s to %d KiB/s", hash, uploadSpeed)
	return nil
}

func (c *Deluge) CheckTorrentPause(ctx context.Context, t *config.Torrent) (bool, error) {
	match, err := expression.CheckTorrentSingleMatch(ctx, t, c.exp.Pauses)
	if err != nil {
		return false, fmt.Errorf("check pause expression: %v: %w", t.Hash, err)
	}

	return match, nil
}

func (c *Deluge) PauseTorrents(ctx context.Context, hashes []string) error {
	var err error
	if c.V2 {
		err = c.client2.PauseTorrents(ctx, hashes...)
	} else {
		err = c.client1.PauseTorrents(ctx, hashes...)
	}

	if err != nil {
		return fmt.Errorf("pause torrents: %v: %w", hashes, err)
	}

	return nil
}
