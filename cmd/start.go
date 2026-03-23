package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var daemon bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the WeChat polling service",
	Run: func(cmd *cobra.Command, args []string) {
		runtimeDir := getRuntimeDir()
		store := wechat.NewAccountStore(runtimeDir)
		acc, err := store.LoadAccount()
		if err != nil || acc.Token == "" {
			fmt.Println("[error] 未找到登录凭证，请先执行 wechat-codex login")
			os.Exit(1)
		}

		if daemon {
			exe, _ := os.Executable()
			startArgs := []string{"start"} // start without -d to run in foreground in child
			c := exec.Command(exe, startArgs...)
			
			logFile, err := os.OpenFile(filepath.Join(runtimeDir, "service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				fmt.Printf("[error] 无法打开日志文件: %v\n", err)
				os.Exit(1)
			}
			c.Stdout = logFile
			c.Stderr = logFile

			err = c.Start()
			if err != nil {
				fmt.Printf("[error] 无法启动后台进程: %v\n", err)
				os.Exit(1)
			}

			pidFile := filepath.Join(runtimeDir, "wechat-codex.pid")
			os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", c.Process.Pid)), 0644)
			fmt.Printf("[ok] 服务已在后台启动，PID: %d\n", c.Process.Pid)
			os.Exit(0)
		}

		fmt.Println("[info] Starting WeChat webhook polling service in foreground...")
		client := wechat.NewClient(acc.BaseURL, acc.Token)
		service := wechat.NewCodexService(client, store)
		service.RunForever()
	},
}

func init() {
	startCmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run in background as daemon")
	rootCmd.AddCommand(startCmd)
}
