package cmd

import (
	"os"
	"wechat-codex/output"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "wechat-codex",
	Short: "Go version of wechat-codex for WeChat bot operations",
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		output.Errorf("%v", err)
		os.Exit(1)
	}
}
