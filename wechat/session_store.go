package wechat

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SessionMeta struct {
	SessionID      string
	Timestamp      string
	Cwd            string
	FilePath       string
	Title          string
	CompletedTurns int
}

type Message struct {
	Role    string
	Content string
}

type SessionStore struct {
	Root string
}

func NewSessionStore(root string) *SessionStore {
	if strings.HasPrefix(root, "~/") {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, root[2:])
	}
	return &SessionStore{Root: root}
}

func (s *SessionStore) walkJsonl() ([]string, error) {
	var files []string
	err := filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func compactText(text string, limit int) string {
	text = strings.TrimSpace(text)
	text = strings.Join(strings.Fields(text), " ")
	runeText := []rune(text)
	if len(runeText) <= limit {
		return string(runeText)
	}
	return string(runeText[:limit-1]) + "…"
}

func normalizeSessionDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}

func parseSessionFile(path string) (*SessionMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Some lines can be very long (e.g. big code blocks)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	if !scanner.Scan() {
		return nil, err
	}

	var firstLine map[string]interface{}
	if err := json.Unmarshal(scanner.Bytes(), &firstLine); err != nil {
		return nil, err
	}

	if firstLine["type"] != "session_meta" {
		return nil, nil
	}

	payload, ok := firstLine["payload"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	sessionID, _ := payload["id"].(string)
	if sessionID == "" {
		return nil, nil
	}

	timestamp, _ := payload["timestamp"].(string)
	cwd, _ := payload["cwd"].(string)
	cwd = normalizeSessionDir(cwd)

	meta := &SessionMeta{
		SessionID: sessionID,
		Timestamp: timestamp,
		Cwd:       cwd,
		FilePath:  path,
	}

	for scanner.Scan() {
		var evt map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &evt); err == nil {
			if evt["type"] == "event_msg" {
				p, ok := evt["payload"].(map[string]interface{})
				if ok {
					msgType, _ := p["type"].(string)
					if meta.Title == "" && msgType == "user_message" {
						if msg, ok := p["message"].(string); ok && strings.TrimSpace(msg) != "" {
							meta.Title = compactText(msg, 46)
						}
					}
					if msgType == "task_complete" {
						meta.CompletedTurns++
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if meta.Title == "" {
		meta.Title = defaultSessionTitle(sessionID)
	}

	return meta, nil
}

func (s *SessionStore) ListRecent(limit int) ([]SessionMeta, error) {
	return s.listRecentMatching(limit, func(*SessionMeta) bool { return true })
}

func (s *SessionStore) ListRecentByCwd(cwd string, limit int) ([]SessionMeta, error) {
	target := normalizeSessionDir(cwd)
	return s.listRecentMatching(limit, func(meta *SessionMeta) bool {
		return meta != nil && meta.Cwd == target
	})
}

func (s *SessionStore) listRecentMatching(limit int, match func(*SessionMeta) bool) ([]SessionMeta, error) {
	files, err := s.walkJsonl()
	if err != nil || len(files) == 0 {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		iInfo, _ := os.Stat(files[i])
		jInfo, _ := os.Stat(files[j])
		if iInfo != nil && jInfo != nil {
			return iInfo.ModTime().After(jInfo.ModTime())
		}
		return false
	})

	var sessions []SessionMeta
	for _, f := range files {
		meta, _ := parseSessionFile(f)
		if meta != nil && match(meta) {
			sessions = append(sessions, *meta)
			if limit > 0 && len(sessions) >= limit {
				break
			}
		}
	}
	return sessions, nil
}

func (s *SessionStore) FindByID(sessionID string) *SessionMeta {
	files, _ := s.walkJsonl()
	for _, f := range files {
		meta, _ := parseSessionFile(f)
		if meta != nil && meta.SessionID == sessionID {
			return meta
		}
	}
	return nil
}

func (s *SessionStore) GetHistory(sessionID string, limit int) (*SessionMeta, []Message) {
	meta := s.FindByID(sessionID)
	if meta == nil {
		return nil, nil
	}

	file, err := os.Open(meta.FilePath)
	if err != nil {
		return meta, nil
	}
	defer file.Close()

	var messages []Message
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var evt map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &evt); err == nil {
			if evt["type"] == "event_msg" {
				p, ok := evt["payload"].(map[string]interface{})
				if ok {
					msgType, _ := p["type"].(string)
					if msgType == "user_message" || msgType == "agent_message" || msgType == "assistant_message" {
						msgStr, _ := p["message"].(string)
						msgStr = strings.TrimSpace(msgStr)
						if msgStr != "" {
							role := "assistant"
							if msgType == "user_message" {
								role = "user"
							}
							messages = append(messages, Message{Role: role, Content: msgStr})
						}
					}
				}
			}
		}
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	return meta, messages
}

func CompactMessage(text string, limit int) string {
	return compactText(text, limit)
}

func defaultSessionTitle(sessionID string) string {
	if sessionID == "" {
		return "session"
	}
	title := "session " + sessionID
	if len(title) > 16 {
		return "session " + sessionID[:8]
	}
	return title
}
