package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the background polling service",
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
			fmt.Println("[info] PID 文件内容无效，服务可能未运行")
			return
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Println("[info] 服务未运行")
			return
		}

		err = process.Signal(syscall.Signal(0))
		if err == nil {
			fmt.Printf("[ok] 服务正在后台运行，PID: %d\n", pid)
		} else {
			fmt.Println("[info] 服务未运行 (进程已退出)")
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
