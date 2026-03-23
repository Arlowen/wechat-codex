package cmd

import (
	"fmt"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to WeChat via QR Code",
	Run: func(cmd *cobra.Command, args []string) {
		err := wechat.LoginFlow(getRuntimeDir(), wechat.DefaultWechatBaseURL, "3")
		if err != nil {
			fmt.Printf("[error] %v\n", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
