package cmd

import (
	"bufio"
	"os"

	"github.com/autobrr/tqm/pkg/runtime"

	"github.com/blang/semver"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
	"github.com/spf13/cobra"
)

func UpdateCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "update",
		Short: "Update to latest version",
		Long:  `This command can be used to self-update to the latest version.`,
	}

	var (
		flagFilterName string
	)

	command.Flags().StringVar(&flagFilterName, "filter", "", "Filter to use instead of client")

	command.Run = func(cmd *cobra.Command, args []string) {
		// init core
		initCore(false)

		// parse current version
		v, err := semver.Parse(runtime.Version)
		if err != nil {
			Log.WithError(err).Fatal("Failed parsing current build version")
		}

		// detect latest version
		Log.Info("Checking for the latest version...")
		latest, found, err := selfupdate.DetectLatest("autobrr/tqm")
		if err != nil {
			Log.WithError(err).Fatal("Failed determining latest available version")
		}

		// check version
		if !found || latest.Version.LTE(v) {
			Log.Infof("Already using the latest version: %v", runtime.Version)
			return
		}

		// ask update
		Log.Infof("Do you want to update to the latest version: %v? (y/n):", latest.Version)
		input, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || (input != "y\n" && input != "n\n") {
			Log.Fatal("Failed validating input...")
		} else if input == "n\n" {
			return
		}

		// get existing executable path
		exe, err := os.Executable()
		if err != nil {
			Log.WithError(err).Fatal("Failed locating current executable path")
		}

		if err := selfupdate.UpdateTo(latest.AssetURL, exe); err != nil {
			Log.WithError(err).Fatal("Failed updating existing binary to latest release")
		}

		Log.Infof("Successfully updated to the latest version: %v", latest.Version)
	}

	return command
}
