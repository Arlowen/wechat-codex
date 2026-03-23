package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"wechat-codex/wechat"

	"github.com/spf13/cobra"
)

const defaultLoginBotType = "3"

type startConfig struct {
	RuntimeDir           string
	BaseURL              string
	LoginBotType         string
	RequireAllowlist     bool
	AllowedUsers         []string
	DefaultCwd           string
	PollTimeoutSec       int
	SendTyping           bool
	CodexBin             string
	SessionsRoot         string
	SandboxMode          string
	ApprovalPolicy       string
	DangerousBypassLevel int
	IdleTimeout          time.Duration
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseBoolEnv(raw string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	default:
		return defaultValue
	}
}

func parseNonNegativeInt(raw string, defaultValue int) int {
	if strings.TrimSpace(raw) == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return defaultValue
	}
	return value
}

func parseDangerousBypassLevel(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	level, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("CODEX_DANGEROUS_BYPASS must be 0, 1, or 2")
	}
	if level < 0 {
		level = 0
	}
	if level > 2 {
		level = 2
	}
	return level, nil
}

func splitAndTrimCSV(raw string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func expandPath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if value == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return value
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	return value
}

func normalizeDirPath(raw string) string {
	value := expandPath(raw)
	if value == "" {
		return ""
	}
	if abs, err := filepath.Abs(value); err == nil {
		value = abs
	}
	return filepath.Clean(value)
}

func defaultRuntimeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wechat-codex"
	}
	return filepath.Join(home, ".wechat-codex")
}

func getRuntimeDir() (string, error) {
	if configured := normalizeDirPath(os.Getenv("WECHAT_RUNTIME_DIR")); configured != "" {
		return configured, nil
	}
	return defaultRuntimeDir(), nil
}

func resolveStringOption(cmd *cobra.Command, flagName, flagValue, envName, defaultValue string) string {
	if flagName != "" && cmd.Flags().Changed(flagName) {
		return strings.TrimSpace(flagValue)
	}
	return firstNonEmpty(os.Getenv(envName), defaultValue)
}

func resolveCodexBin(configured string) string {
	value := strings.TrimSpace(configured)
	if value != "" {
		value = expandPath(value)
		if strings.ContainsRune(value, os.PathSeparator) {
			if abs, err := filepath.Abs(value); err == nil {
				return abs
			}
		}
		return value
	}
	if found, err := exec.LookPath("codex"); err == nil {
		return found
	}
	appPath := "/Applications/Codex.app/Contents/Resources/codex"
	if _, err := os.Stat(appPath); err == nil {
		return appPath
	}
	return "codex"
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func pidFilePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "wechat-codex.pid")
}

func logFilePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "service.log")
}

func liveServicePID(runtimeDir string, currentPID int) (int, bool, error) {
	data, err := os.ReadFile(pidFilePath(runtimeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(pidFilePath(runtimeDir))
		return 0, false, nil
	}

	if pid == currentPID {
		return pid, false, nil
	}
	if processExists(pid) {
		return pid, true, nil
	}

	_ = os.Remove(pidFilePath(runtimeDir))
	return 0, false, nil
}

func finalAllowedUsers(configured []string, requireAllowlist bool, loginUserID string) ([]string, error) {
	allowed := splitAndTrimCSV(strings.Join(configured, ","))
	if requireAllowlist && len(allowed) == 0 && strings.TrimSpace(loginUserID) != "" {
		allowed = append(allowed, strings.TrimSpace(loginUserID))
	}
	if requireAllowlist && len(allowed) == 0 {
		return nil, errors.New(
			"ALLOWED_WECHAT_USER_IDS is required by default for safety. " +
				"Set your WeChat user ID, or set WECHAT_REQUIRE_ALLOWLIST=0 to override",
		)
	}
	return allowed, nil
}

func filterDaemonArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case arg == "-d", arg == "--daemon":
			continue
		case strings.HasPrefix(arg, "--daemon="):
			continue
		case strings.HasPrefix(arg, "-d="):
			continue
		default:
			filtered = append(filtered, arg)
		}
	}
	return filtered
}

func resolveStartConfig(cmd *cobra.Command) (startConfig, error) {
	runtimeDir, err := getRuntimeDir()
	if err != nil {
		return startConfig{}, fmt.Errorf("resolve runtime dir: %w", err)
	}

	requireAllowlist := parseBoolEnv(os.Getenv("WECHAT_REQUIRE_ALLOWLIST"), true)
	allowedRaw := resolveStringOption(cmd, "allowed-users", allowedUsers, "ALLOWED_WECHAT_USER_IDS", "")
	baseURL := firstNonEmpty(os.Getenv("WECHAT_API_BASE_URL"), wechat.DefaultWechatBaseURL)
	loginBotType := firstNonEmpty(os.Getenv("WECHAT_LOGIN_BOT_TYPE"), defaultLoginBotType)
	defaultCwd := normalizeDirPath(firstNonEmpty(os.Getenv("DEFAULT_CWD")))
	if defaultCwd == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return startConfig{}, fmt.Errorf("resolve default cwd: %w", err)
		}
		defaultCwd = normalizeDirPath(cwd)
	}
	if info, err := os.Stat(defaultCwd); err != nil || !info.IsDir() {
		return startConfig{}, fmt.Errorf("DEFAULT_CWD does not exist or is not a directory: %s", defaultCwd)
	}

	pollTimeoutSec := parseNonNegativeInt(firstNonEmpty(os.Getenv("WECHAT_POLL_TIMEOUT_SEC"), "35"), 35)
	if pollTimeoutSec < 5 {
		pollTimeoutSec = 5
	}
	sendTyping := parseBoolEnv(os.Getenv("WECHAT_SEND_TYPING"), true)

	sessionsRoot := normalizeDirPath(resolveStringOption(cmd, "sessions", sessionsDir, "CODEX_SESSION_ROOT", "~/.codex/sessions"))
	codexBin := resolveCodexBin(resolveStringOption(cmd, "codex-bin", codexBin, "CODEX_BIN", ""))
	dangerousBypassLevel, err := parseDangerousBypassLevel(firstNonEmpty(os.Getenv("CODEX_DANGEROUS_BYPASS"), "0"))
	if err != nil {
		return startConfig{}, err
	}
	idleTimeoutSec := parseNonNegativeInt(
		firstNonEmpty(os.Getenv("CODEX_IDLE_TIMEOUT_SEC"), os.Getenv("CODEX_EXEC_TIMEOUT_SEC"), "3600"),
		3600,
	)

	return startConfig{
		RuntimeDir:           runtimeDir,
		BaseURL:              baseURL,
		LoginBotType:         loginBotType,
		RequireAllowlist:     requireAllowlist,
		AllowedUsers:         splitAndTrimCSV(allowedRaw),
		DefaultCwd:           defaultCwd,
		PollTimeoutSec:       pollTimeoutSec,
		SendTyping:           sendTyping,
		CodexBin:             codexBin,
		SessionsRoot:         sessionsRoot,
		SandboxMode:          strings.TrimSpace(os.Getenv("CODEX_SANDBOX_MODE")),
		ApprovalPolicy:       strings.TrimSpace(os.Getenv("CODEX_APPROVAL_POLICY")),
		DangerousBypassLevel: dangerousBypassLevel,
		IdleTimeout:          time.Duration(idleTimeoutSec) * time.Second,
	}, nil
}
