package cmd

import (
	"os"
	"syscall"

	"wechat-codex/output"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background polling service",
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
		if !running {
			output.Infof("服务未运行")
			return
		}

		process, err := os.FindProcess(pid)
		if err == nil {
			err = process.Signal(syscall.SIGTERM)
			if err == nil {
				output.OKf("成功发送终止信号给进程 PID: %d", pid)
			} else {
				output.Warnf("终止进程失败 或者进程已不存在: %v", err)
			}
		}

		_ = os.Remove(pidFilePath(runtimeDir))

		accountStore := wechat.NewAccountStore(runtimeDir)
		account, err := accountStore.LoadAccount()
		if err == nil && account.Token != "" {
			output.OKf("微信登录凭证仍保留")
		}
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
