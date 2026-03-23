package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestFilterDaemonArgs(t *testing.T) {
	args := []string{
		"start",
		"-d",
		"--allowed-users=a,b",
		"--codex-bin",
		"/tmp/codex",
		"--daemon=true",
		"--sessions=/tmp/sessions",
	}

	filtered := filterDaemonArgs(args)
	want := []string{
		"start",
		"--allowed-users=a,b",
		"--codex-bin",
		"/tmp/codex",
		"--sessions=/tmp/sessions",
	}

	if len(filtered) != len(want) {
		t.Fatalf("unexpected filtered args length: got %v want %v", filtered, want)
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("unexpected filtered arg[%d]: got %q want %q", i, filtered[i], want[i])
		}
	}
}

func TestDefaultRuntimeDirFromExecutable(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		want string
	}{
		{
			name: "binary inside bin dir",
			exe:  "/tmp/project/bin/wechat-codex",
			want: "/tmp/project/.runtime/wechat",
		},
		{
			name: "binary next to runtime dir",
			exe:  "/tmp/project/wechat-codex",
			want: "/tmp/project/.runtime/wechat",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultRuntimeDirFromExecutable(tc.exe); got != tc.want {
				t.Fatalf("unexpected runtime dir: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFinalAllowedUsersDefaultsToLoginUser(t *testing.T) {
	allowed, err := finalAllowedUsers(nil, true, "user@im.wechat")
	if err != nil {
		t.Fatalf("finalAllowedUsers returned error: %v", err)
	}
	if len(allowed) != 1 || allowed[0] != "user@im.wechat" {
		t.Fatalf("unexpected allowed users: %#v", allowed)
	}
}

func TestLiveServicePIDDetectsRunningProcess(t *testing.T) {
	runtimeDir := t.TempDir()
	pid := os.Getpid()
	if err := os.WriteFile(pidFilePath(runtimeDir), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	gotPID, running, err := liveServicePID(runtimeDir, 0)
	if err != nil {
		t.Fatalf("liveServicePID returned error: %v", err)
	}
	if !running {
		t.Fatal("expected liveServicePID to report running process")
	}
	if gotPID != pid {
		t.Fatalf("unexpected pid: got %d want %d", gotPID, pid)
	}
}

func TestGetRuntimeDirHonorsEnvironment(t *testing.T) {
	t.Setenv("WECHAT_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime", "wechat"))

	got, err := getRuntimeDir()
	if err != nil {
		t.Fatalf("getRuntimeDir returned error: %v", err)
	}
	if got != filepath.Clean(os.Getenv("WECHAT_RUNTIME_DIR")) {
		t.Fatalf("unexpected runtime dir: got %q want %q", got, filepath.Clean(os.Getenv("WECHAT_RUNTIME_DIR")))
	}
}
