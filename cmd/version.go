package cmd

import (
	"wechat-codex/output"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		output.Infof("version: %s", Version)
		if Commit != "" && Commit != "unknown" {
			output.Infof("commit: %s", Commit)
		}
		if BuildDate != "" && BuildDate != "unknown" {
			output.Infof("built at: %s", BuildDate)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
