package cmd

import (
	"encoding/json"
	"time"

	"github.com/autobrr/tqm/client"
	"github.com/autobrr/tqm/config"
	"github.com/autobrr/tqm/expression"
	"github.com/autobrr/tqm/hardlinkfilemap"
	"github.com/autobrr/tqm/logger"
	"github.com/autobrr/tqm/sliceutils"
	"github.com/autobrr/tqm/torrentfilemap"
	"github.com/autobrr/tqm/tracker"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var relabelCmd = &cobra.Command{
	Use:   "relabel [CLIENT]",
	Short: "Check torrent client for torrents to relabel",
	Long:  `This command can be used to check a torrent clients queue for torrents to relabel based on its configured filters.`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// init core
		if !initialized {
			initCore(true)
			initialized = true
		}

		// set log
		log := logger.GetLogger("relabel")

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

		// get free disk space (can/will be used by filters)
		if clientFreeSpacePath != nil {
			space, err := c.GetCurrentFreeSpace(*clientFreeSpacePath)
			if err != nil {
				log.WithError(err).Warnf("Failed retrieving free-space for: %q", *clientFreeSpacePath)
			} else {
				log.Infof("Retrieved free-space for %q: %v (%.2f GB)", *clientFreeSpacePath,
					humanize.IBytes(uint64(space)), c.GetFreeSpace())
			}
		} else if *clientType == "qbittorrent" {
			// For qBittorrent, we can get free space without a path
			space, err := c.GetCurrentFreeSpace("")
			if err != nil {
				log.WithError(err).Warn("Failed retrieving free-space")
			} else {
				log.Infof("Retrieved free-space: %v (%.2f GB)",
					humanize.IBytes(uint64(space)), c.GetFreeSpace())
			}
		}

		// load client label path map
		if err := c.LoadLabelPathMap(); err != nil {
			log.WithError(err).Fatal("Failed loading label path map")
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

		if sliceutils.StringSliceContains(clientFilter.MapHardlinksFor, "relabel", true) {
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
			log.Warnf("If your setup involves multiple torrents sharing the same underlying file using hardlinks, or you are using the 'HardlinkedOutsideClient' field in your filters, you should add 'relabel' to the 'MapHardlinksFor' field in your filter configuration")
		}

		// relabel torrents that meet the filter criteria
		if err := relabelEligibleTorrents(log, c, torrents, tfm); err != nil {
			log.WithError(err).Fatal("Failed relabeling eligible torrents...")
		}
	},
}

func init() {
	rootCmd.AddCommand(relabelCmd)

	relabelCmd.Flags().StringVar(&flagFilterName, "filter", "", "Filter to use instead of client")
}
