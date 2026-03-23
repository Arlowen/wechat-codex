package cmd

import (
	"fmt"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the background polling service",
	Run: func(cmd *cobra.Command, args []string) {
		runtimeDir, err := getRuntimeDir()
		if err != nil {
			fmt.Printf("[error] 无法确定 runtime 目录: %v\n", err)
			return
		}

		pid, running, err := liveServicePID(runtimeDir, 0)
		if err != nil {
			fmt.Printf("[error] 无法检查服务状态: %v\n", err)
			return
		}

		if running {
			fmt.Printf("[ok] 服务正在后台运行，PID: %d\n", pid)
		} else {
			fmt.Println("[info] 服务未运行")
		}

		accountStore := wechat.NewAccountStore(runtimeDir)
		account, err := accountStore.LoadAccount()
		if err == nil && account.Token != "" {
			fmt.Println("[ok] 已检测到微信登录凭证")
		} else {
			fmt.Println("[info] 尚未检测到微信登录凭证")
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
