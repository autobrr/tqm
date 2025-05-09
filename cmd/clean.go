package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/expression"
	"github.com/autobrr/tqm/pkg/hardlinkfilemap"
	"github.com/autobrr/tqm/pkg/logger"
	"github.com/autobrr/tqm/pkg/sliceutils"
	"github.com/autobrr/tqm/pkg/torrentfilemap"
	"github.com/autobrr/tqm/pkg/tracker"
)

var cleanCmd = &cobra.Command{
	Use:   "clean [CLIENT]",
	Short: "Check torrent client for torrents to remove",
	Long:  `This command can be used to check a torrent clients queue for torrents to remove based on its configured filters.`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		// init core
		if !initialized {
			initCore(true)
			initialized = true
		}

		// set log
		log := logger.GetLogger("clean")

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
		if err := c.Connect(ctx); err != nil {
			log.WithError(err).Fatal("Failed connecting")
		} else {
			log.Debugf("Connected to client")
		}

		// get free disk space (can/will be used by filters)
		switch *clientType {
		case "qbittorrent":
			// For qBittorrent, we can get free space without a path
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
				filterUsesFreespace := checkFilterUsesFreespace(clientFilter)

				if filterUsesFreespace {
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

		if flagLogLevel > 1 {
			if _, err := json.Marshal(torrents); err != nil {
				log.WithError(err).Error("Failed marshalling torrents")
			}
		}

		// create map of files associated to torrents (via hash)
		tfm := torrentfilemap.New(torrents)
		log.Infof("Mapped torrents to %d unique torrent files", tfm.Length())

		var hfm hardlinkfilemap.HardlinkFileMapI
		if sliceutils.StringSliceContains(clientFilter.MapHardlinksFor, "clean", true) {
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
			hfm = hardlinkfilemap.New(torrents, clientDownloadPathMapping)
			log.Infof("Mapped all torrent file paths to %d unique underlying file IDs in %s", hfm.Length(), time.Since(start))

			// add HardlinkedOutsideClient field to torrents
			for h, t := range torrents {
				t.HardlinkedOutsideClient = hfm.HardlinkedOutsideClient(t)
				torrents[h] = t
			}
		} else {
			log.Warnf("Not mapping hardlinks for client %q", clientName)
			log.Warnf("If your setup involves multiple torrents sharing the same underlying file using hardlinks, or you are using the 'HardlinkedOutsideClient' field in your filters, you should add 'clean' to the 'MapHardlinksFor' field in your filter configuration")
			hfm = hardlinkfilemap.NewNoopHardlinkFileMap()
		}

		// remove torrents that are not ignored and match remove criteria
		if err := removeEligibleTorrents(ctx, log, c, torrents, tfm, hfm, clientFilter); err != nil {
			log.WithError(err).Fatal("Failed removing eligible torrents...")
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)

	cleanCmd.Flags().StringVar(&flagFilterName, "filter", "", "Filter to use instead of client")
}

// checkFilterUsesFreespace checks if any filter conditions use FreeSpaceGB or FreeSpaceSet
func checkFilterUsesFreespace(filter *config.FilterConfiguration) bool {
	// Helper function to check a single expression for free space usage
	checkExpression := func(expr string) bool {
		return strings.Contains(expr, "FreeSpaceGB") || strings.Contains(expr, "FreeSpaceSet")
	}

	// Check all filter conditions
	for _, expr := range filter.Ignore {
		if checkExpression(expr) {
			return true
		}
	}
	for _, expr := range filter.Remove {
		if checkExpression(expr) {
			return true
		}
	}

	// Check label expressions
	for _, label := range filter.Label {
		for _, expr := range label.Update {
			if checkExpression(expr) {
				return true
			}
		}
	}

	// Check tag expressions
	for _, tag := range filter.Tag {
		for _, expr := range tag.Update {
			if checkExpression(expr) {
				return true
			}
		}
	}

	return false
}
