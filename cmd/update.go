package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/blang/semver"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
	"github.com/spf13/cobra"

	"github.com/autobrr/tqm/pkg/runtime"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update to latest version",
	Long:  `This command can be used to self-update to the latest version.`,

	Run: func(cmd *cobra.Command, args []string) {
		// parse current version
		v, err := semver.Parse(runtime.Version)
		if err != nil {
			fmt.Printf("Failed parsing current build version: %v\n", err)
			os.Exit(1)
		}

		// detect latest version
		fmt.Println("Checking for the latest version...")
		latest, found, err := selfupdate.DetectLatest("autobrr/tqm")
		if err != nil {
			fmt.Printf("Failed determining latest available version: %v\n", err)
			os.Exit(1)
		}

		// check version
		if !found || latest.Version.LTE(v) {
			fmt.Printf("Already using the latest version: %v\n", runtime.Version)
			return
		}

		// ask update
		fmt.Printf("Do you want to update to the latest version: %v? (y/n):\n", latest.Version)
		input, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || (input != "y\n" && input != "n\n") {
			fmt.Println("Failed validating input...")
			os.Exit(1)
		} else if input == "n\n" {
			return
		}

		// get existing executable path
		exe, err := os.Executable()
		if err != nil {
			fmt.Printf("Failed locating current executable path: %v\n", err)
			os.Exit(1)
		}

		if err := selfupdate.UpdateTo(latest.AssetURL, exe); err != nil {
			fmt.Printf("Failed updating existing binary to latest release: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully updated to the latest version: %v\n", latest.Version)
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
