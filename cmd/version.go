package cmd

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/runtime"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Prints the version, commit hash, and build date for the tqm binary.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version: %s\n", runtime.Version)
		fmt.Printf("Commit:  %s\n", runtime.GitCommit)
		if runtime.Timestamp != "" && runtime.Timestamp != "unknown" {
			unixTime, err := strconv.ParseInt(runtime.Timestamp, 10, 64)
			if err == nil {
				buildTime := time.Unix(unixTime, 0)
				fmt.Printf("Build Time:  %s\n", buildTime.Format(time.RFC3339))
			} else {
				fmt.Printf("Build Time:  %s (raw)\n", runtime.Timestamp)
			}
		}
	},
	DisableFlagsInUseLine: true,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
