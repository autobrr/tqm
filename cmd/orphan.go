package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/notification"
	"github.com/autobrr/tqm/pkg/paths"
	"github.com/autobrr/tqm/pkg/torrentfilemap"
	"github.com/autobrr/tqm/pkg/tracker"
)

var (
	orphanCategories        []string
	orphanExcludeCategories []string
	orphanUseCategoryPaths  bool
)

var orphanCmd = &cobra.Command{
	Use:   "orphan [CLIENT]",
	Short: "Check download location for orphan files/folders not in torrent client",
	Long:  `This command can be used to find files and folders in the download_location that are no longer in the torrent client.`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		start := time.Now()

		// init core
		if !initialized {
			initCore(true)
			initialized = true
		}

		// set log
		log := logger.GetLogger("orphan")

		noti := notification.NewDiscordSender(log, config.Config.Notifications)

		// retrieve client object
		clientName := args[0]
		clientConfig, ok := config.Config.Clients[clientName]
		if !ok {
			log.Fatalf("No client configuration found for: %q", clientName)
		}

		// validate client is enabled
		if err := validateClientEnabled(clientConfig); err != nil {
			log.WithError(err).Fatal("Failed validating client is enabled")
		}

		// retrieve client type
		clientType, err := getClientConfigString("type", clientConfig)
		if err != nil {
			log.WithError(err).Fatal("Failed determining client type")
		}

		// retrieve client download path
		clientDownloadPath, err := getClientConfigString("download_path", clientConfig)
		if err != nil {
			log.WithError(err).Fatal("Failed determining client download path")
		} else if clientDownloadPath == nil || *clientDownloadPath == "" {
			log.Fatal("Client download path must be set...")
		}

		// retrieve client download path mapping
		clientDownloadPathMapping, err := getClientDownloadPathMapping(clientConfig)
		if err != nil {
			log.WithError(err).Fatal("Failed loading client download path mappings")
		} else if clientDownloadPathMapping != nil {
			log.Debugf("Loaded %d client download path mappings: %#v", len(clientDownloadPathMapping),
				clientDownloadPathMapping)
		}

		// load client object
		c, err := client.NewClient(*clientType, clientName, nil)
		if err != nil {
			log.WithError(err).Fatalf("Failed initializing client: %q", clientName)
		}

		log.Infof("Initialized client %q, type: %s (%d trackers)", clientName, c.Type(), tracker.Loaded())

		// connect to client
		if err := c.Connect(ctx); err != nil {
			log.WithError(err).Fatal("Failed connecting")
		} else {
			log.Debugf("Connected to client")
		}

		// Use category-aware orphan detection if enabled
		if orphanUseCategoryPaths {
			// Load label path map first
			if err := c.LoadLabelPathMap(ctx); err != nil {
				log.WithError(err).Fatal("Failed loading label path map")
			}
			processCategoryAwareOrphans(ctx, c, clientDownloadPathMapping, log, noti, clientName, start)
			return
		}

		// Legacy mode: load label path map and get torrents
		if err := c.LoadLabelPathMap(ctx); err != nil {
			log.WithError(err).Fatal("Failed loading label path map")
		}

		// retrieve torrents
		torrents, err := c.GetTorrents(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed retrieving torrents")
		} else {
			log.Infof("Retrieved %d torrents", len(torrents))
		}

		// Legacy mode: filter torrents by category if specified
		filteredTorrents := filterTorrentsByCategory(torrents, orphanCategories, orphanExcludeCategories, log)
		if len(filteredTorrents) != len(torrents) {
			log.Infof("Filtered to %d torrents based on category filters", len(filteredTorrents))
		}

		// create map of files associated with torrents (via hash)
		tfm := torrentfilemap.New(filteredTorrents)
		log.Infof("Mapped torrents to %d unique torrent files", tfm.Length())

		// get all paths in client download location
		localDownloadPaths, _ := paths.InFolder(*clientDownloadPath, true, true,
			nil)
		log.Tracef("Retrieved %d paths from: %q", len(localDownloadPaths), *clientDownloadPath)

		// sort paths into their respective maps
		localFilePaths := make(map[string]int64)
		localFolderPaths := make(map[string]int64)

		for _, p := range localDownloadPaths {
			if p.IsDir {
				if strings.EqualFold(p.RealPath, *clientDownloadPath) {
					// ignore root download path
					continue
				}

				localFolderPaths[p.RealPath] = p.Size
			} else {
				localFilePaths[p.RealPath] = p.Size
			}
		}

		log.Infof("Retrieved paths from %q: %d files / %d folders", *clientDownloadPath, len(localFilePaths),
			len(localFolderPaths))

		const (
			maxWorkers = 10
			batchSize  = 50
		)

		var (
			wg                    sync.WaitGroup
			mu                    sync.Mutex
			removeFailures        atomic.Uint32
			removedLocalFiles     atomic.Uint32
			ignoredLocalFiles     atomic.Uint32
			removedLocalFilesSize atomic.Uint64
			fields                []notification.Field
		)

		filter, err := getClientFilter(clientConfig)
		if err != nil {
			log.WithError(err).Fatal("Failed to get client filter")
		}

		if filter == nil {
			log.Fatal("Defined filter is empty")
		}

		gracePeriod := 10 * time.Minute
		if filter.Orphan.GracePeriod > 0 {
			gracePeriod = filter.Orphan.GracePeriod
		}
		log.Debugf("Using grace period: %v", gracePeriod)

		processInBatches(localFilePaths, maxWorkers, batchSize, func(localPath string, localPathSize int64) {
			defer wg.Done()

			if tfm.HasPath(localPath, clientDownloadPathMapping) {
				return
			}

			if paths.IsIgnored(localPath, filter.Orphan.IgnorePaths) {
				mu.Lock()
				log.Debugf("File matches a path in the ignore list, skipping removal: %q", localPath)
				mu.Unlock()
				ignoredLocalFiles.Add(1)
				return
			}

			// check file modification time for grace period
			fileInfo, err := os.Stat(localPath)
			if err != nil {
				mu.Lock()
				log.WithError(err).Warnf("Could not stat file, skipping removal check: %q", localPath)
				mu.Unlock()
				return
			}

			if time.Since(fileInfo.ModTime()) < gracePeriod {
				mu.Lock()
				log.Warnf("File is recently modified (within %v), skipping removal due to grace period: %q", gracePeriod, localPath)
				mu.Unlock()
				return
			}

			mu.Lock()
			log.Info("-----")
			log.Infof("Removing orphan (outside grace period): %q", localPath)
			mu.Unlock()

			removed := true

			if flagDryRun {
				mu.Lock()
				log.Warn("Dry-run enabled, skipping remove...")
				mu.Unlock()
			} else {
				if err := os.Remove(localPath); err != nil {
					mu.Lock()
					log.WithError(err).Errorf("Failed removing orphan...")
					mu.Unlock()
					removeFailures.Add(1)
					removed = false
				} else {
					mu.Lock()
					log.Info("Removed")
					mu.Unlock()
				}
			}

			if removed {
				removedLocalFilesSize.Add(uint64(localPathSize))
				removedLocalFiles.Add(1)

				mu.Lock()
				fields = append(fields, noti.BuildField(notification.ActionOrphan, notification.BuildOptions{
					Orphan:     localPath,
					OrphanSize: localPathSize,
					IsFile:     true,
				}))
				mu.Unlock()
			}
		}, &wg)

		wg.Wait()

		var ignoredLocalFolders uint32
		orphanFolderPaths := make([]string, 0, len(localFolderPaths))
		for localPath := range localFolderPaths {
			if tfm.HasPath(localPath, clientDownloadPathMapping) {
				continue
			}

			if paths.IsIgnored(localPath, filter.Orphan.IgnorePaths) {
				log.Debugf("Folder matches a path in the ignore list, skipping removal: %q", localPath)
				ignoredLocalFolders++
				continue
			}

			orphanFolderPaths = append(orphanFolderPaths, localPath)
		}

		// Sort orphan folders by path length (depth) in descending order
		// This ensures deepest directories are processed first
		sort.Slice(orphanFolderPaths, func(i, j int) bool {
			return len(orphanFolderPaths[i]) > len(orphanFolderPaths[j])
		})

		log.Debugf("Processing %d potential orphan folders, sorted by depth", len(orphanFolderPaths))

		var removedLocalFolders uint32
		for _, localPath := range orphanFolderPaths {
			log.Info("-----")
			log.Infof("Checking orphan folder: %q", localPath)

			removed := false

			empty, err := paths.IsDirEmpty(localPath)
			if err != nil {
				log.WithError(err).Warnf("Could not check if directory is empty, skipping removal: %q", localPath)
			} else if !empty {
				log.Warnf("Orphan directory is not empty, skipping removal: %q", localPath)
			} else {
				log.Infof("Attempting to remove empty orphan directory: %q", localPath)
				if flagDryRun {
					log.Warn("Dry-run enabled, skipping remove...")
					removed = true
				} else {
					if err := os.Remove(localPath); err != nil {
						log.WithError(err).Errorf("Failed removing empty orphan directory...")
						removeFailures.Add(1)
					} else {
						log.Info("Removed empty orphan directory")
						removed = true
					}
				}
			}

			if removed {
				fields = append(fields, noti.BuildField(notification.ActionOrphan, notification.BuildOptions{
					Orphan:     localPath,
					OrphanSize: 0,
					IsFile:     false,
				}))
				removedLocalFolders++
			}
		}

		log.Info("-----")
		log.WithField("reclaimed_space", humanize.IBytes(removedLocalFilesSize.Load())).
			Infof("Removed orphans: %d files, %d folders and %d failures. Ignored %d files and %d folders",
				removedLocalFiles.Load(), removedLocalFolders, removeFailures.Load(), ignoredLocalFiles.Load(), ignoredLocalFolders)

		if !noti.CanSend() {
			log.Debug("Notifications disabled, skipping...")
			return
		}

		sendErr := noti.Send(
			"Orphans",
			fmt.Sprintf("Removed **%d** orphaned files and **%d** orphaned folders | Total reclaimed **%s**",
				removedLocalFiles.Load(), removedLocalFolders, humanize.IBytes(removedLocalFilesSize.Load())),
			clientName,
			time.Since(start),
			fields,
			flagDryRun,
		)
		if sendErr != nil {
			log.WithError(sendErr).Error("Failed sending notification")
		}
	},
}

// processCategoryAwareOrphans checks each category's save path for orphaned files
func processCategoryAwareOrphans(ctx context.Context, c client.Interface,
	clientDownloadPathMapping map[string]string, log *logrus.Entry, noti notification.Sender, clientName string, start time.Time) {

	labelPathMap := c.LabelPathMap()
	if len(labelPathMap) == 0 {
		log.Warn("No categories found in client, nothing to check")
		return
	}

	log.Infof("Found %d categories to check", len(labelPathMap))

	filter, err := getClientFilter(config.Config.Clients[clientName])
	if err != nil {
		log.WithError(err).Fatal("Failed to get client filter")
	}

	if filter == nil {
		log.Fatal("Defined filter is empty")
	}

	gracePeriod := 10 * time.Minute
	if filter.Orphan.GracePeriod > 0 {
		gracePeriod = filter.Orphan.GracePeriod
	}
	log.Debugf("Using grace period: %v", gracePeriod)

	var (
		totalRemovedFiles     uint32
		totalRemovedFolders   uint32
		totalIgnoredFiles     uint32
		totalIgnoredFolders   uint32
		totalRemoveFailures   uint32
		totalReclaimedBytes   uint64
		allFields             []notification.Field
	)

	// Process each category
	for categoryName, categoryPath := range labelPathMap {
		// Skip categories based on include/exclude filters
		if len(orphanCategories) > 0 {
			found := false
			for _, cat := range orphanCategories {
				if strings.EqualFold(categoryName, cat) {
					found = true
					break
				}
			}
			if !found {
				log.Debugf("Skipping category %q (not in include list)", categoryName)
				continue
			}
		}

		if len(orphanExcludeCategories) > 0 {
			excluded := false
			for _, cat := range orphanExcludeCategories {
				if strings.EqualFold(categoryName, cat) {
					excluded = true
					break
				}
			}
			if excluded {
				log.Debugf("Skipping category %q (in exclude list)", categoryName)
				continue
			}
		}

		log.Info("==========")
		log.Infof("Checking category: %q with path: %q", categoryName, categoryPath)

		// Check if category path exists
		if _, err := os.Stat(categoryPath); os.IsNotExist(err) {
			log.Warnf("Category path does not exist: %q", categoryPath)
			continue
		}

		// Get all paths in category location FIRST
		log.Debugf("Scanning filesystem for category %q...", categoryName)
		localPaths, _ := paths.InFolder(categoryPath, true, true, nil)
		log.Debugf("Retrieved %d paths from category path", len(localPaths))

		// Sort paths
		localFilePaths := make(map[string]int64)
		localFolderPaths := make(map[string]int64)

		for _, p := range localPaths {
			if p.IsDir {
				if strings.EqualFold(p.RealPath, categoryPath) {
					continue
				}
				localFolderPaths[p.RealPath] = p.Size
			} else {
				localFilePaths[p.RealPath] = p.Size
			}
		}

		log.Infof("Category %q: %d files / %d folders on disk", categoryName, len(localFilePaths), len(localFolderPaths))

		// NOW get torrents to confirm what should be kept
		log.Debugf("Fetching current torrent list from client...")
		allTorrents, err := c.GetTorrents(ctx)
		if err != nil {
			log.WithError(err).Errorf("Failed retrieving torrents for category %q, skipping", categoryName)
			continue
		}

		// Filter torrents to only those in this category
		categoryTorrents := make(map[string]config.Torrent)
		for hash, torrent := range allTorrents {
			if strings.EqualFold(torrent.Label, categoryName) {
				categoryTorrents[hash] = torrent
			}
		}

		log.Infof("Category has %d active torrents", len(categoryTorrents))

		if len(categoryTorrents) == 0 && (len(localFilePaths) > 0 || len(localFolderPaths) > 0) {
			log.Warnf("Category %q has files/folders but no torrents - use with caution!", categoryName)
		}

		// Create file map for this category's torrents only
		tfm := torrentfilemap.New(categoryTorrents)
		log.Debugf("Mapped %d unique torrent files for category %q", tfm.Length(), categoryName)

		// Process orphaned files
		removedFiles, ignoredFiles, removeFailures, reclaimedBytes, fields := processOrphanFiles(
			localFilePaths, tfm, clientDownloadPathMapping, filter, gracePeriod, log, noti)

		totalRemovedFiles += removedFiles
		totalIgnoredFiles += ignoredFiles
		totalRemoveFailures += removeFailures
		totalReclaimedBytes += reclaimedBytes
		allFields = append(allFields, fields...)

		// Process orphaned folders
		removedFolders, ignoredFolders, folderFailures, folderFields := processOrphanFolders(
			localFolderPaths, tfm, clientDownloadPathMapping, filter, categoryPath, log, noti)

		totalRemovedFolders += removedFolders
		totalIgnoredFolders += ignoredFolders
		totalRemoveFailures += folderFailures
		allFields = append(allFields, folderFields...)

		log.Infof("Category %q: removed %d files, %d folders", categoryName, removedFiles, removedFolders)
	}

	log.Info("==========")
	log.WithField("reclaimed_space", humanize.IBytes(totalReclaimedBytes)).
		Infof("Total removed orphans: %d files, %d folders and %d failures. Ignored %d files and %d folders",
			totalRemovedFiles, totalRemovedFolders, totalRemoveFailures, totalIgnoredFiles, totalIgnoredFolders)

	if !noti.CanSend() {
		log.Debug("Notifications disabled, skipping...")
		return
	}

	sendErr := noti.Send(
		"Orphans (Category-Aware)",
		fmt.Sprintf("Removed **%d** orphaned files and **%d** orphaned folders | Total reclaimed **%s**",
			totalRemovedFiles, totalRemovedFolders, humanize.IBytes(totalReclaimedBytes)),
		clientName,
		time.Since(start),
		allFields,
		flagDryRun,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}
}

// processOrphanFiles processes orphaned files and returns stats
func processOrphanFiles(localFilePaths map[string]int64, tfm *torrentfilemap.TorrentFileMap,
	clientDownloadPathMapping map[string]string, filter *config.FilterConfiguration,
	gracePeriod time.Duration, log *logrus.Entry, noti notification.Sender) (
	removedFiles uint32, ignoredFiles uint32, removeFailures uint32, reclaimedBytes uint64, fields []notification.Field) {

	const (
		maxWorkers = 10
		batchSize  = 50
	)

	var (
		wg                    sync.WaitGroup
		mu                    sync.Mutex
		removedFilesAtomic    atomic.Uint32
		ignoredFilesAtomic    atomic.Uint32
		removeFailuresAtomic  atomic.Uint32
		reclaimedBytesAtomic  atomic.Uint64
		fieldsLocal           []notification.Field
	)

	processInBatches(localFilePaths, maxWorkers, batchSize, func(localPath string, localPathSize int64) {
		defer wg.Done()

		// Double-check the file is still an orphan before removing
		if tfm.HasPath(localPath, clientDownloadPathMapping) {
			mu.Lock()
			log.Debugf("File is tracked by a torrent, skipping: %q", localPath)
			mu.Unlock()
			return
		}

		if paths.IsIgnored(localPath, filter.Orphan.IgnorePaths) {
			mu.Lock()
			log.Debugf("File matches ignore list, skipping: %q", localPath)
			mu.Unlock()
			ignoredFilesAtomic.Add(1)
			return
		}

		fileInfo, err := os.Stat(localPath)
		if err != nil {
			mu.Lock()
			log.WithError(err).Warnf("Could not stat file, skipping: %q", localPath)
			mu.Unlock()
			return
		}

		if time.Since(fileInfo.ModTime()) < gracePeriod {
			mu.Lock()
			log.Debugf("File within grace period, skipping: %q", localPath)
			mu.Unlock()
			return
		}

		mu.Lock()
		log.Infof("Removing orphan file: %q", localPath)
		mu.Unlock()

		removed := true

		if flagDryRun {
			mu.Lock()
			log.Warn("Dry-run enabled, skipping remove...")
			mu.Unlock()
		} else {
			if err := os.Remove(localPath); err != nil {
				mu.Lock()
				log.WithError(err).Errorf("Failed removing orphan file")
				mu.Unlock()
				removeFailuresAtomic.Add(1)
				removed = false
			} else {
				mu.Lock()
				log.Info("Removed")
				mu.Unlock()
			}
		}

		if removed {
			reclaimedBytesAtomic.Add(uint64(localPathSize))
			removedFilesAtomic.Add(1)

			mu.Lock()
			fieldsLocal = append(fieldsLocal, noti.BuildField(notification.ActionOrphan, notification.BuildOptions{
				Orphan:     localPath,
				OrphanSize: localPathSize,
				IsFile:     true,
			}))
			mu.Unlock()
		}
	}, &wg)

	wg.Wait()

	return removedFilesAtomic.Load(), ignoredFilesAtomic.Load(), removeFailuresAtomic.Load(),
		reclaimedBytesAtomic.Load(), fieldsLocal
}

// processOrphanFolders processes orphaned folders and returns stats
func processOrphanFolders(localFolderPaths map[string]int64, tfm *torrentfilemap.TorrentFileMap,
	clientDownloadPathMapping map[string]string, filter *config.FilterConfiguration,
	categoryPath string, log *logrus.Entry, noti notification.Sender) (
	removedFolders uint32, ignoredFolders uint32, removeFailures uint32, fields []notification.Field) {

	orphanFolderPaths := make([]string, 0, len(localFolderPaths))
	for localPath := range localFolderPaths {
		if tfm.HasPath(localPath, clientDownloadPathMapping) {
			continue
		}

		if paths.IsIgnored(localPath, filter.Orphan.IgnorePaths) {
			log.Debugf("Folder matches ignore list, skipping: %q", localPath)
			ignoredFolders++
			continue
		}

		orphanFolderPaths = append(orphanFolderPaths, localPath)
	}

	// Sort by depth (deepest first)
	sort.Slice(orphanFolderPaths, func(i, j int) bool {
		return len(orphanFolderPaths[i]) > len(orphanFolderPaths[j])
	})

	log.Debugf("Processing %d potential orphan folders", len(orphanFolderPaths))

	for _, localPath := range orphanFolderPaths {
		removed := false

		empty, err := paths.IsDirEmpty(localPath)
		if err != nil {
			log.WithError(err).Warnf("Could not check if directory is empty: %q", localPath)
		} else if !empty {
			log.Debugf("Orphan directory not empty, skipping: %q", localPath)
		} else {
			log.Infof("Removing empty orphan directory: %q", localPath)
			if flagDryRun {
				log.Warn("Dry-run enabled, skipping remove...")
				removed = true
			} else {
				if err := os.Remove(localPath); err != nil {
					log.WithError(err).Errorf("Failed removing empty orphan directory")
					removeFailures++
				} else {
					log.Info("Removed")
					removed = true
				}
			}
		}

		if removed {
			fields = append(fields, noti.BuildField(notification.ActionOrphan, notification.BuildOptions{
				Orphan:     localPath,
				OrphanSize: 0,
				IsFile:     false,
			}))
			removedFolders++
		}
	}

	return removedFolders, ignoredFolders, removeFailures, fields
}

// filterTorrentsByCategory filters torrents based on category include/exclude lists
func filterTorrentsByCategory(torrents map[string]config.Torrent, includeCategories, excludeCategories []string, log *logrus.Entry) map[string]config.Torrent {
	// if no filters specified, return all torrents
	if len(includeCategories) == 0 && len(excludeCategories) == 0 {
		return torrents
	}

	filtered := make(map[string]config.Torrent)

	for hash, torrent := range torrents {
		category := torrent.Label

		// check exclude list first
		if len(excludeCategories) > 0 {
			excluded := false
			for _, excludeCat := range excludeCategories {
				if strings.EqualFold(category, excludeCat) {
					log.Tracef("Excluding torrent %q with category %q", torrent.Name, category)
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		// check include list
		if len(includeCategories) > 0 {
			included := false
			for _, includeCat := range includeCategories {
				if strings.EqualFold(category, includeCat) {
					included = true
					break
				}
			}
			if !included {
				log.Tracef("Skipping torrent %q with category %q (not in include list)", torrent.Name, category)
				continue
			}
		}

		filtered[hash] = torrent
	}

	return filtered
}

// processInBatches processes a map in batches using a worker pool
func processInBatches(items map[string]int64, maxWorkers int, batchSize int,
	processFn func(string, int64), wg *sync.WaitGroup) {

	workerSem := make(chan struct{}, maxWorkers)

	i := 0
	batch := make([]struct {
		key string
		val int64
	}, 0, batchSize)

	for k, v := range items {
		batch = append(batch, struct {
			key string
			val int64
		}{k, v})
		i++

		// when batch is full or all items are accumulated, process the batch
		if i == batchSize || i == len(items) {
			for _, item := range batch {
				wg.Add(1)

				workerSem <- struct{}{}

				go func(path string, size int64) {
					defer func() {
						<-workerSem
					}()

					processFn(path, size)
				}(item.key, item.val)
			}

			batch = batch[:0]
		}
	}
}

func init() {
	rootCmd.AddCommand(orphanCmd)
	
	orphanCmd.Flags().BoolVar(&orphanUseCategoryPaths, "use-category-paths", false, "Check each category's save path for orphans (qBittorrent only)")
	orphanCmd.Flags().StringSliceVar(&orphanCategories, "category", nil, "Only check specific categories (works with --use-category-paths or legacy mode)")
	orphanCmd.Flags().StringSliceVar(&orphanExcludeCategories, "exclude-category", nil, "Exclude specific categories from orphan check")
}
