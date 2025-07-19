package cmd

import (
	"fmt"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/pkg/runtime"
)

const repoSlug = "autobrr/tqm"

var updateCmd = &cobra.Command{
	Use:                   "update",
	Short:                 "Update tqm",
	Long:                  `Update tqm to latest version.`,
	DisableFlagsInUseLine: true,

	RunE: func(cmd *cobra.Command, args []string) error {
		release, err := selfupdate.UpdateSelf(cmd.Context(), runtime.Version, selfupdate.ParseSlug(repoSlug))
		if err != nil {
			return fmt.Errorf("could not update binary: %w", err)
		}

		fmt.Printf("Successfully updated to version: %s\n", release.Version())
		return nil
	},
}

func init() {
	updateCmd.SetUsageTemplate(`Usage:
  {{.CommandPath}}
  
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
`)

	rootCmd.AddCommand(updateCmd)
}
