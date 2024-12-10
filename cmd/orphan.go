package cmd

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/pathutils"
	"github.com/autobrr/tqm/pkg/torrentfilemap"
	"github.com/autobrr/tqm/pkg/tracker"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func OrphanCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "orphan [CLIENT]",
		Short: "Check download location for orphan files/folders not in torrent client",
		Long:  `This command can be used to find files and folders in the download_location that are no longer in the torrent client.`,
	}

	var (
		flagFilterName string
	)

	command.Flags().StringVar(&flagFilterName, "filter", "", "Filter to use instead of client")

	command.Run = func(cmd *cobra.Command, args []string) {
		// init core
		if !Initialized {
			initCore(true)
			Initialized = true
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

		if FlagLogLevel > 1 {
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

		// remove local files not associated with a torrent
		removeFailures := 0
		removedLocalFiles := 0
		var removedLocalFilesSize uint64 = 0

		for localPath, localPathSize := range localFilePaths {
			if tfm.HasPath(localPath, clientDownloadPathMapping) {
				continue
			} else {
				log.Info("-----")

				// file is not associated with a torrent
				removed := true

				log.Infof("Removing orphan: %q", localPath)
				if FlagDryRun {
					log.Warn("Dry-run enabled, skipping remove...")
				} else {
					// remove file
					if err := os.Remove(localPath); err != nil {
						log.WithError(err).Errorf("Failed removing orphan...")
						removeFailures++
						removed = false
					} else {
						log.Info("Removed")
					}
				}

				if removed {
					removedLocalFilesSize += uint64(localPathSize)
					removedLocalFiles++
				}
			}
		}

		// remove local folders not associated with a torrent
		removedLocalFolders := 0

		for localPath := range localFolderPaths {
			if tfm.HasPath(localPath, clientDownloadPathMapping) {
				continue
			} else {
				log.Info("-----")

				// folder is not associated with a torrent
				removed := true

				log.Infof("Removing orphan: %q", localPath)
				if FlagDryRun {
					log.Warn("Dry-run enabled, skipping remove...")
				} else {
					// remove folder
					if err := os.Remove(localPath); err != nil {
						log.WithError(err).Errorf("Failed removing orphan...")
						removeFailures++
						removed = false
					} else {
						log.Info("Removed")
					}
				}

				if removed {
					removedLocalFolders++
				}
			}
		}

		log.Info("-----")
		log.WithField("reclaimed_space", humanize.IBytes(removedLocalFilesSize)).
			Infof("Removed orphans: %d files, %d folders and %d failures",
				removedLocalFiles, removedLocalFolders, removeFailures)
	}

	return command
}
