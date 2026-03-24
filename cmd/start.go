package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"wechat-codex/output"
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
		config, err := resolveStartConfig(cmd)
		if err != nil {
			output.Errorf("启动配置无效: %v", err)
			os.Exit(1)
		}

		if !parseBoolEnv(os.Getenv("WECHAT_ENABLED"), true) {
			output.Infof("微信服务已被 WECHAT_ENABLED=0 显式关闭")
			return
		}

		if err := os.MkdirAll(config.RuntimeDir, 0o755); err != nil {
			output.Errorf("无法创建 runtime 目录: %v", err)
			os.Exit(1)
		}

		if pid, running, err := liveServicePID(config.RuntimeDir, os.Getpid()); err != nil {
			output.Errorf("无法检查现有服务状态: %v", err)
			os.Exit(1)
		} else if running {
			output.Infof("服务已运行，PID: %d", pid)
			return
		}

		store := wechat.NewAccountStore(config.RuntimeDir)
		acc, err := store.LoadAccount()
		if err != nil || acc.Token == "" {
			output.Infof("当前为第一次启动，需要先扫描二维码绑定微信")
			if err := wechat.LoginFlow(config.RuntimeDir, config.BaseURL, config.LoginBotType); err != nil {
				output.Errorf("扫码登录中止: %v", err)
				os.Exit(1)
			}
			acc, err = store.LoadAccount()
			if err != nil || acc.Token == "" {
				output.Errorf("未能正确获取登录凭证")
				os.Exit(1)
			}
		}

		allowedUsersResolved, err := finalAllowedUsers(config.AllowedUsers, config.RequireAllowlist, acc.UserID)
		if err != nil {
			output.Errorf("启动配置无效: %v", err)
			os.Exit(1)
		}

		if daemon {
			exe, err := os.Executable()
			if err != nil {
				output.Errorf("无法定位当前可执行文件: %v", err)
				os.Exit(1)
			}

			startArgs := filterDaemonArgs(os.Args[1:])
			if len(startArgs) == 0 {
				startArgs = []string{"start"}
			}
			c := exec.Command(exe, startArgs...)
			c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

			logFile, err := os.OpenFile(logFilePath(config.RuntimeDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
			if err != nil {
				output.Errorf("无法打开日志文件: %v", err)
				os.Exit(1)
			}
			c.Stdout = logFile
			c.Stderr = logFile

			err = c.Start()
			if err != nil {
				_ = logFile.Close()
				output.Errorf("无法启动后台进程: %v", err)
				os.Exit(1)
			}
			_ = logFile.Close()

			if err := os.WriteFile(pidFilePath(config.RuntimeDir), []byte(fmt.Sprintf("%d", c.Process.Pid)), 0o644); err != nil {
				output.Errorf("无法写入 PID 文件: %v", err)
				os.Exit(1)
			}

			time.Sleep(2 * time.Second)
			if !processExists(c.Process.Pid) {
				_ = os.Remove(pidFilePath(config.RuntimeDir))
				output.Errorf("后台进程启动后立即退出，请检查日志")
				output.Infof("日志: %s", logFilePath(config.RuntimeDir))
				os.Exit(1)
			}

			output.Infof("服务已在后台启动，PID: %d", c.Process.Pid)
			output.Infof("日志: %s", logFilePath(config.RuntimeDir))
			return
		}

		output.Infof("Starting WeChat webhook polling service in foreground...")
		baseURL := acc.BaseURL
		if baseURL == "" {
			baseURL = config.BaseURL
		}
		client := wechat.NewClient(baseURL, acc.Token)

		sessions := wechat.NewSessionStore(config.SessionsRoot)

		botState := wechat.NewBotState(config.RuntimeDir)
		codexRunner := wechat.NewCodexRunner(config.CodexBin)
		codexRunner.SandboxMode = config.SandboxMode
		codexRunner.ApprovalPolicy = config.ApprovalPolicy
		codexRunner.DangerousBypassLevel = config.DangerousBypassLevel
		codexRunner.IdleTimeout = config.IdleTimeout

		service := wechat.NewCodexService(
			client,
			store,
			sessions,
			botState,
			codexRunner,
			config.DefaultCwd,
			allowedUsersResolved,
			config.PollTimeoutSec,
			config.SendTyping,
		)
		service.RunForever()
	},
}

func init() {
	startCmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run in background as daemon")
	startCmd.Flags().StringVar(&codexBin, "codex-bin", "codex", "Path to codex binary")
	startCmd.Flags().StringVar(&sessionsDir, "sessions", "~/.codex/sessions", "Path to codex session tracking directory")
	startCmd.Flags().StringVar(&allowedUsers, "allowed-users", "", "Comma separated list of allowed WeChat user IDs")
	rootCmd.AddCommand(startCmd)
}
