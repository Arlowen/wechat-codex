package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background polling service",
	Run: func(cmd *cobra.Command, args []string) {
		runtimeDir := getRuntimeDir()
		pidFile := filepath.Join(runtimeDir, "wechat-codex.pid")

		data, err := os.ReadFile(pidFile)
		if err != nil {
			fmt.Println("[info] 服务未运行 (找不到 PID 文件)")
			return
		}

		pid, err := strconv.Atoi(string(data))
		if err != nil {
			fmt.Println("[info] PID 文件内容无效")
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

		os.Remove(pidFile)
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
