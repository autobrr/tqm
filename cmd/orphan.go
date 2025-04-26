package cmd

import (
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/client"
	"github.com/autobrr/tqm/config"
	"github.com/autobrr/tqm/logger"
	paths "github.com/autobrr/tqm/pathutils"
	"github.com/autobrr/tqm/torrentfilemap"
	"github.com/autobrr/tqm/tracker"
)

var orphanCmd = &cobra.Command{
	Use:   "orphan [CLIENT]",
	Short: "Check download location for orphan files/folders not in torrent client",
	Long:  `This command can be used to find files and folders in the download_location that are no longer in the torrent client.`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// init core
		if !initialized {
			initCore(true)
			initialized = true
		}

		// set log
		log := logger.GetLogger("orphan")

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
		if err := c.Connect(); err != nil {
			log.WithError(err).Fatal("Failed connecting")
		} else {
			log.Debugf("Connected to client")
		}

		// retrieve torrents
		torrents, err := c.GetTorrents()
		if err != nil {
			log.WithError(err).Fatal("Failed retrieving torrents")
		} else {
			log.Infof("Retrieved %d torrents", len(torrents))
		}

		if flagLogLevel > 1 {
			if b, err := json.Marshal(torrents); err != nil {
				log.WithError(err).Error("Failed marshalling torrents")
			} else {
				log.Trace(string(b))
			}
		}

		// create map of files associated to torrents (via hash)
		tfm := torrentfilemap.New(torrents)
		log.Infof("Mapped torrents to %d unique torrent files", tfm.Length())

		// get all paths in client download location
		localDownloadPaths, _ := paths.GetPathsInFolder(*clientDownloadPath, true, true,
			nil)
		log.Tracef("Retrieved %d paths from: %q", len(localDownloadPaths), *clientDownloadPath)

		// sort paths into their respective maps
		localFilePaths := make(map[string]int64)
		localFolderPaths := make(map[string]int64)

		for _, p := range localDownloadPaths {
			p := p
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

		const maxWorkers = 10
		const batchSize = 50

		var wg sync.WaitGroup
		var mu sync.Mutex
		var atomicRemoveFailures uint32
		var atomicRemovedLocalFiles uint32
		var atomicRemovedLocalFilesSize uint64

		gracePeriod := 10 * time.Minute
		if config.Config.Orphan.GracePeriod > 0 {
			gracePeriod = config.Config.Orphan.GracePeriod
		}
		log.Debugf("Using grace period: %v", gracePeriod)

		processInBatches(localFilePaths, maxWorkers, batchSize, func(localPath string, localPathSize int64) {
			defer wg.Done()

			if tfm.HasPath(localPath, clientDownloadPathMapping) {
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
					atomic.AddUint32(&atomicRemoveFailures, 1)
					removed = false
				} else {
					mu.Lock()
					log.Info("Removed")
					mu.Unlock()
				}
			}

			if removed {
				atomic.AddUint64(&atomicRemovedLocalFilesSize, uint64(localPathSize))
				atomic.AddUint32(&atomicRemovedLocalFiles, 1)
			}
		}, &wg)

		wg.Wait()

		orphanFolderPaths := make([]string, 0, len(localFolderPaths))
		for localPath := range localFolderPaths {
			if !tfm.HasPath(localPath, clientDownloadPathMapping) {
				orphanFolderPaths = append(orphanFolderPaths, localPath)
			}
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

			empty, err := isDirEmpty(localPath)
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
						atomic.AddUint32(&atomicRemoveFailures, 1)
					} else {
						log.Info("Removed empty orphan directory")
						removed = true
					}
				}
			}

			if removed {
				removedLocalFolders++
			}
		}

		removeFailures := atomic.LoadUint32(&atomicRemoveFailures)
		removedLocalFiles := atomic.LoadUint32(&atomicRemovedLocalFiles)
		removedLocalFilesSize := atomic.LoadUint64(&atomicRemovedLocalFilesSize)

		log.Info("-----")
		log.WithField("reclaimed_space", humanize.IBytes(removedLocalFilesSize)).
			Infof("Removed orphans: %d files, %d folders and %d failures",
				removedLocalFiles, removedLocalFolders, removeFailures)
	},
}

func isDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Read exactly one entry. If EOF, the directory is empty.
	// If we get any entry, it's not empty. Poetry.
	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return false, nil
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
}
