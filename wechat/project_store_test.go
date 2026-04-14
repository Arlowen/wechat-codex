package wechat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectStoreListProjects(t *testing.T) {
	root := t.TempDir()
	projectA := filepath.Join(root, "project-a")
	projectB := filepath.Join(root, "project-b")
	if err := os.MkdirAll(projectA, 0o755); err != nil {
		t.Fatalf("mkdir project a: %v", err)
	}
	if err := os.MkdirAll(projectB, 0o755); err != nil {
		t.Fatalf("mkdir project b: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}

	configPath := filepath.Join(root, "config.toml")
	content := `
[projects."/"]
trust_level = "trusted"

[projects."` + home + `"]
trust_level = "trusted"

[projects."` + projectA + `"]
trust_level = "trusted"

[projects."` + filepath.Join(root, "missing") + `"]
trust_level = "trusted"

[projects."` + projectA + `/"]
trust_level = "trusted"

[projects."` + projectB + `"]
trust_level = "trusted"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := NewProjectStore(configPath)
	projects, err := store.ListProjects(10)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %#v", projects)
	}
	if projects[0] != filepath.Clean(projectA) {
		t.Fatalf("unexpected first project: %q", projects[0])
	}
	if projects[1] != filepath.Clean(projectB) {
		t.Fatalf("unexpected second project: %q", projects[1])
	}
}
