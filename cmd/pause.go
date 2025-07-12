package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/expression"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/notification"
	"github.com/autobrr/tqm/pkg/tracker"
)

var pauseCmd = &cobra.Command{
	Use:   "pause [CLIENT]",
	Short: "Check torrent client for torrents to pause",
	Long:  `This command can be used to check a torrent client's queue for torrents to pause based on its configured filters.`,

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
		log := logger.GetLogger("pause")

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

		// retrieve client free space path (needed for Deluge free space check)
		clientFreeSpacePath, _ := getClientConfigString("free_space_path", clientConfig)

		// retrieve client filters
		clientFilter, err := getClientFilter(clientConfig)
		if err != nil {
			log.WithError(err).Fatal("Failed retrieving client filter")
		}

		if flagFilterName != "" {
			clientFilter, err = getFilter(flagFilterName)
			if err != nil {
				log.WithError(err).Fatal("Failed retrieving specified filter")
			}
		}

		// compile client filters
		exp, err := expression.Compile(clientFilter)
		if err != nil {
			log.WithError(err).Fatal("Failed compiling client filters")
		}

		// load client object
		c, err := client.NewClient(*clientType, clientName, exp)
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

		// get free disk space (can/will be used by filters)
		switch *clientType {
		case "qbittorrent":
			space, err := c.GetCurrentFreeSpace(ctx, "")
			if err != nil {
				log.WithError(err).Error("Failed retrieving free-space")
			} else {
				log.Infof("Retrieved free-space: %v (%.2f GB)",
					humanize.IBytes(uint64(space)), c.GetFreeSpace())
			}

		case "deluge":
			if clientFreeSpacePath != nil {
				space, err := c.GetCurrentFreeSpace(ctx, *clientFreeSpacePath)
				if err != nil {
					log.WithError(err).Errorf("Failed retrieving free-space for: %q", *clientFreeSpacePath)
					os.Exit(1)
				} else {
					log.Infof("Retrieved free-space for %q: %v (%.2f GB)", *clientFreeSpacePath,
						humanize.IBytes(uint64(space)), c.GetFreeSpace())
				}
			} else {
				if filterUsesFreeSpace(clientFilter) {
					log.Error("Deluge requires free_space_path to be configured in order to retrieve free space information")
					os.Exit(1)
				}
			}
		}

		// retrieve torrents
		torrents, err := c.GetTorrents(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed retrieving torrents")
		} else {
			log.Infof("Retrieved %d torrents", len(torrents))
		}

		var (
			pauseList []string
			fields    []notification.Field
		)

		// iterate through torrents
		for _, t := range torrents {
			// check if torrent should be ignored
			if ignored, err := c.ShouldIgnore(ctx, &t); err != nil {
				log.WithError(err).Errorf("Failed checking ignore filters for torrent: %q", t.Name)
				continue
			} else if ignored {
				log.Debugf("Ignoring torrent: %q", t.Name)
				continue
			}

			// check if torrent should be paused
			if paused, err := c.CheckTorrentPause(ctx, &t); err != nil {
				log.WithError(err).Errorf("Failed checking pause filters for torrent: %q", t.Name)
				continue
			} else if paused {
				log.Infof("Adding torrent to pause list: %q", t.Name)
				pauseList = append(pauseList, t.Hash)
				fields = append(fields, noti.BuildField(notification.ActionPause, notification.BuildOptions{
					Torrent: t,
				}))
			}
		}

		// pause torrents if not dry run
		if !flagDryRun {
			if len(pauseList) > 0 {
				log.Infof("Pausing %d torrent(s)...", len(pauseList))
				if err := c.PauseTorrents(ctx, pauseList); err != nil {
					log.WithError(err).Fatalf("Failed pausing torrents: %v", err)
				}
				log.Infof("Successfully paused %d torrent(s)", len(pauseList))
			} else {
				log.Info("No torrents to pause")
			}
		} else {
			if len(pauseList) > 0 {
				log.Infof("[DRY-RUN] Would pause %d torrent(s)", len(pauseList))
			} else {
				log.Info("[DRY-RUN] No torrents would be paused")
			}
		}

		if !noti.CanSend() {
			log.Debug("Notifications disabled, skipping...")
			return
		}

		sendErr := noti.Send(
			"Torrent Pause",
			fmt.Sprintf("Paused **%d** torrent(s)", len(pauseList)),
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

func init() {
	rootCmd.AddCommand(pauseCmd)

	pauseCmd.Flags().StringVar(&flagFilterName, "filter", "", "Filter to use instead of client")
}
