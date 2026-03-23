package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

var daemon bool
var codexBin string
var sessionsDir string
var allowedUsers string

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
		
		sessionsRoot := sessionsDir
		if sessionsRoot == "" {
			sessionsRoot = "~/.cursor-tutor/sessions"
		}
		sessions := wechat.NewSessionStore(sessionsRoot)
		
		botState := wechat.NewBotState(runtimeDir)
		codexRunner := wechat.NewCodexRunner(codexBin)
		
		var allowed []string
		if allowedUsers != "" {
			allowed = strings.Split(allowedUsers, ",")
		}
		
		cwd, _ := os.Getwd()
		
		service := wechat.NewCodexService(
			client,
			store,
			sessions,
			botState,
			codexRunner,
			cwd,
			allowed,
			30,
			true,
		)
		service.RunForever()
	},
}

func init() {
	startCmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run in background as daemon")
	startCmd.Flags().StringVar(&codexBin, "codex-bin", "codex", "Path to codex binary")
	startCmd.Flags().StringVar(&sessionsDir, "sessions", "~/.cursor-tutor/sessions", "Path to codex session tracking directory")
	startCmd.Flags().StringVar(&allowedUsers, "allowed-users", "", "Comma separated list of allowed WeChat user IDs")
	rootCmd.AddCommand(startCmd)
}
