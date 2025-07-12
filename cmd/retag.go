package cmd

import (
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/evaluate"
	"github.com/autobrr/tqm/pkg/expression"
	"github.com/autobrr/tqm/pkg/hardlinkfilemap"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/notification"
	"github.com/autobrr/tqm/pkg/tracker"
)

var retagCmd = &cobra.Command{
	Use:   "retag [CLIENT]",
	Short: "Check client (only qbit) for torrents to retag",
	Long:  `This command can be used to check a torrent clients queue for torrents to retag based on its configured filters.`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		startTime := time.Now()

		// init core
		if !initialized {
			initCore(true)
			initialized = true
		}

		// set log
		log := logger.GetLogger("retag")

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

		if *clientType != "qbittorrent" {
			log.Fatalf("Retagging is currently only supported for qbittorrent")
		}

		// retrieve client free space path
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

		ct, ok := c.(client.TagInterface)
		if !ok {
			log.Fatalf("Retagging is currently only supported for qbittorrent")
		}

		if err != nil {
			log.WithError(err).Fatalf("Failed initializing client: %q", clientName)
		}

		log.Infof("Initialized client %q, type: %s (%d trackers)", clientName, ct.Type(), tracker.Loaded())

		// connect to client
		if err := ct.Connect(ctx); err != nil {
			log.WithError(err).Fatal("Failed connecting")
		} else {
			log.Debugf("Connected to client")
		}

		// get free disk space (can/will be used by filters)
		if clientFreeSpacePath != nil {
			space, err := ct.GetCurrentFreeSpace(ctx, *clientFreeSpacePath)
			if err != nil {
				log.WithError(err).Errorf("Failed retrieving free-space for: %q", *clientFreeSpacePath)
			} else {
				log.Infof("Retrieved free-space for %q: %v (%.2f GB)", *clientFreeSpacePath,
					humanize.IBytes(uint64(space)), ct.GetFreeSpace())
			}
		} else if *clientType == "qbittorrent" {
			// For qBittorrent, we can get free space without a path
			space, err := ct.GetCurrentFreeSpace(ctx, "")
			if err != nil {
				log.WithError(err).Error("Failed retrieving free-space")
			} else {
				log.Infof("Retrieved free-space: %v (%.2f GB)",
					humanize.IBytes(uint64(space)), ct.GetFreeSpace())
			}
		}

		// retrieve torrents
		torrents, err := ct.GetTorrents(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed retrieving torrents")
		} else {
			log.Infof("Retrieved %d torrents", len(torrents))
		}

		if evaluate.StringSliceContains(clientFilter.MapHardlinksFor, "retag", true) {
			// download path mapping
			clientDownloadPathMapping, err := getClientDownloadPathMapping(clientConfig)
			if err != nil {
				log.WithError(err).Fatal("Failed loading client download path mappings")
			} else if clientDownloadPathMapping != nil {
				log.Debugf("Loaded %d client download path mappings: %#v", len(clientDownloadPathMapping),
					clientDownloadPathMapping)
			}

			// create map of paths associated to underlying file ids
			start := time.Now()
			hfm := hardlinkfilemap.New(torrents, clientDownloadPathMapping)
			log.Infof("Mapped all torrent file paths to %d unique underlying file IDs in %s", hfm.Length(), time.Since(start))

			// add HardlinkedOutsideClient field to torrents
			for h, t := range torrents {
				t.HardlinkedOutsideClient = hfm.HardlinkedOutsideClient(t)
				torrents[h] = t
			}
		} else {
			log.Warnf("Not mapping hardlinks for client %q", clientName)
			log.Warnf("If your setup involves multiple torrents sharing the same underlying file using hardlinks, or you are using the 'HardlinkedOutsideClient' field in your filters, you should add 'retag' to the 'MapHardlinksFor' field in your filter configuration")
		}

		// Verify tags exist on client
		var tagList []string
		for _, v := range exp.Tags {
			tagList = append(tagList, v.Name)
		}
		if err := ct.CreateTags(ctx, tagList); err != nil {
			log.WithError(err).Fatal("Failed to create tags on client")
		} else {
			log.Infof("Verified tags exist on client")
		}

		// relabel torrents that meet the filter criteria
		if err := retagEligibleTorrents(ctx, log, ct, torrents, noti, clientName, startTime); err != nil {
			log.WithError(err).Fatal("Failed retagging eligible torrents...")
		}
	},
}

func init() {
	rootCmd.AddCommand(retagCmd)

	retagCmd.Flags().StringVar(&flagFilterName, "filter", "", "Filter to use instead of client")
}
