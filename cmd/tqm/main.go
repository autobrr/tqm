package main

import (
	"fmt"
	"os"

	"github.com/autobrr/tqm/cmd"
	"github.com/spf13/cobra"
)

//var (
//	// Global flags
//	flagLogLevel     = 0
//	flagConfigFile   = "config.yaml"
//	flagConfigFolder = config.GetDefaultConfigDirectory("tqm", flagConfigFile)
//	flagLogFile      = "activity.log"
//
//	flagFilterName                       string
//	flagDryRun                           bool
//	flagExperimentalRelabelForCrossSeeds bool
//
//	// Global vars
//	log         *logrus.Entry
//	initialized bool
//)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "tqm",
		Short: "A CLI torrent queue manager",
		Long: `A CLI application that can be used to manage your torrent clients.
`,
	}

	// Parse persistent flags
	rootCmd.PersistentFlags().StringVar(&cmd.FlagConfigFolder, "config-dir", cmd.FlagConfigFolder, "Config folder")
	rootCmd.PersistentFlags().StringVarP(&cmd.FlagConfigFile, "config", "c", cmd.FlagConfigFile, "Config file")
	rootCmd.PersistentFlags().StringVarP(&cmd.FlagLogFile, "log", "l", cmd.FlagLogFile, "Log file")
	rootCmd.PersistentFlags().CountVarP(&cmd.FlagLogLevel, "verbose", "v", "Verbose level")

	rootCmd.PersistentFlags().BoolVar(&cmd.FlagDryRun, "dry-run", false, "Dry run mode")
	rootCmd.PersistentFlags().BoolVar(&cmd.FlagExperimentalRelabelForCrossSeeds, "experimental-relabel", false, "Enable experimental relabeling for cross-seeded torrents, using hardlinks (only qbit for now")

	rootCmd.AddCommand(cmd.CleanCommand())
	rootCmd.AddCommand(cmd.OrphanCommand())
	rootCmd.AddCommand(cmd.RelabelCommand())
	rootCmd.AddCommand(cmd.RetagCommand())
	rootCmd.AddCommand(cmd.UpdateCommand())
	rootCmd.AddCommand(cmd.VersionCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
