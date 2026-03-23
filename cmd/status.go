package cmd

import (
	"wechat-codex/output"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the background polling service",
	Run: func(cmd *cobra.Command, args []string) {
		runtimeDir, err := getRuntimeDir()
		if err != nil {
			output.Errorf("无法确定 runtime 目录: %v", err)
			return
		}

		pid, running, err := liveServicePID(runtimeDir, 0)
		if err != nil {
			output.Errorf("无法检查服务状态: %v", err)
			return
		}

		if running {
			output.Infof("服务正在后台运行，PID: %d", pid)
		} else {
			output.Infof("服务未运行")
		}

		accountStore := wechat.NewAccountStore(runtimeDir)
		account, err := accountStore.LoadAccount()
		if err == nil && account.Token != "" {
			output.Infof("已检测到微信登录凭证")
		} else {
			output.Infof("尚未检测到微信登录凭证")
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
