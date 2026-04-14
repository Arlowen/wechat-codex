package wechat

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProjectLister interface {
	ListProjects(limit int) ([]string, error)
}

type ProjectStore struct {
	ConfigPath string
}

func NewProjectStore(configPath string) *ProjectStore {
	return &ProjectStore{ConfigPath: strings.TrimSpace(configPath)}
}

func (s *ProjectStore) ListProjects(limit int) ([]string, error) {
	configPath, err := s.resolveConfigPath()
	if err != nil {
		return nil, err
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	home, _ := os.UserHomeDir()
	home = normalizeProjectPath(home)

	seen := make(map[string]bool)
	var projects []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		projectPath, ok := parseProjectHeader(scanner.Text())
		if !ok {
			continue
		}
		projectPath = normalizeProjectPath(projectPath)
		if projectPath == "" || seen[projectPath] {
			continue
		}
		if projectPath == string(os.PathSeparator) {
			continue
		}
		if home != "" && projectPath == home {
			continue
		}
		if !dirExists(projectPath) {
			continue
		}

		seen[projectPath] = true
		projects = append(projects, projectPath)
		if limit > 0 && len(projects) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return projects, nil
}

func (s *ProjectStore) resolveConfigPath() (string, error) {
	if strings.TrimSpace(s.ConfigPath) != "" {
		return filepath.Clean(expandUserPath(s.ConfigPath)), nil
	}

	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for codex config: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}

	codexHome = expandUserPath(codexHome)
	if abs, err := filepath.Abs(codexHome); err == nil {
		codexHome = abs
	}
	return filepath.Join(filepath.Clean(codexHome), "config.toml"), nil
}

func parseProjectHeader(line string) (string, bool) {
	line = strings.TrimSpace(line)
	const prefix = `[projects."`
	const suffix = `"]`
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return "", false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(line, prefix), suffix)
	if value == "" {
		return "", false
	}
	return value, true
}

func normalizeProjectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = expandUserPath(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}
