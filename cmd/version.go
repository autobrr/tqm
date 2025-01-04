package cmd

import (
	"fmt"

	"github.com/autobrr/tqm/pkg/runtime"

	"github.com/spf13/cobra"
)

func VersionCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Long:  `Print version info`,
		Example: `  tqm version
  tqm version --help`,
		//SilenceUsage: true,
	}

	command.RunE = func(cmd *cobra.Command, args []string) error {
		fmt.Printf("tqm version: %s commit: %s built at: %s\n", runtime.Version, runtime.GitCommit, runtime.Timestamp)
		return nil
	}

	return command
}
