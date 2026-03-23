package cmd

import (
	"fmt"
	"os"

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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getRuntimeDir() string {
	cwd, _ := os.Getwd()
	return cwd + "/.runtime/wechat"
}
