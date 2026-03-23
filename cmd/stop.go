package cmd

import (
	"fmt"
	"os"
	"syscall"

	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background polling service",
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
		if !running {
			fmt.Println("[info] 服务未运行")
			return
		}

		process, err := os.FindProcess(pid)
		if err == nil {
			err = process.Signal(syscall.SIGTERM)
			if err == nil {
				fmt.Printf("[ok] 成功发送终止信号给进程 PID: %d\n", pid)
			} else {
				fmt.Printf("[warn] 终止进程失败 或者进程已不存在: %v\n", err)
			}
		}

		_ = os.Remove(pidFilePath(runtimeDir))

		accountStore := wechat.NewAccountStore(runtimeDir)
		account, err := accountStore.LoadAccount()
		if err == nil && account.Token != "" {
			fmt.Println("[ok] 微信登录凭证仍保留")
		}
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
