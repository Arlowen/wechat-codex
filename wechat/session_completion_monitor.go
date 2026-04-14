package wechat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionCompletionPollInterval = 1500 * time.Millisecond
	trackFreshFileWindow                 = 2 * time.Minute
	sessionMonitorStaleAfter             = 5 * time.Minute
)

type SessionCompletionEvent struct {
	SessionID       string
	Cwd             string
	Title           string
	CompletionCount int
}

type trackedCompletionFile struct {
	offset                  int64
	partial                 string
	sessionID               string
	cwd                     string
	title                   string
	lastRelevantEvent       string
	completionCount         int
	notifiedCompletionCount int
	lastEventTime           time.Time
}

type SessionCompletionMonitor struct {
	root                 string
	pollInterval         time.Duration
	onCompletion         func(SessionCompletionEvent)
	now                  func() time.Time
	stopCh               chan struct{}
	doneCh               chan struct{}
	started              bool
	tracked              map[string]*trackedCompletionFile
	sessionNotifiedCount map[string]int
	mu                   sync.Mutex
}

func NewSessionCompletionMonitor(root string, pollInterval time.Duration, onCompletion func(SessionCompletionEvent)) *SessionCompletionMonitor {
	if pollInterval <= 0 {
		pollInterval = defaultSessionCompletionPollInterval
	}
	return &SessionCompletionMonitor{
		root:                 filepath.Clean(expandUserPath(root)),
		pollInterval:         pollInterval,
		onCompletion:         onCompletion,
		now:                  time.Now,
		tracked:              make(map[string]*trackedCompletionFile),
		sessionNotifiedCount: make(map[string]int),
	}
}

func (m *SessionCompletionMonitor) Start() {
	m.mu.Lock()
	if m.stopCh != nil {
		m.mu.Unlock()
		return
	}
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})
	stopCh := m.stopCh
	doneCh := m.doneCh
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(m.pollInterval)
		defer ticker.Stop()
		defer close(doneCh)

		m.poll()
		for {
			select {
			case <-ticker.C:
				m.poll()
			case <-stopCh:
				return
			}
		}
	}()
}

func (m *SessionCompletionMonitor) Stop() {
	m.mu.Lock()
	stopCh := m.stopCh
	doneCh := m.doneCh
	if stopCh == nil {
		m.mu.Unlock()
		return
	}
	m.stopCh = nil
	m.doneCh = nil
	m.mu.Unlock()

	close(stopCh)
	<-doneCh
}

func (m *SessionCompletionMonitor) MarkNotified(sessionID string, completionCount int) {
	if strings.TrimSpace(sessionID) == "" || completionCount <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordSessionNotifiedLocked(sessionID, completionCount)
	for _, tracked := range m.tracked {
		if tracked.sessionID == sessionID && tracked.notifiedCompletionCount < completionCount {
			tracked.notifiedCompletionCount = completionCount
		}
	}
}

func (m *SessionCompletionMonitor) poll() {
	now := m.now()

	m.mu.Lock()
	initialScan := !m.started
	var events []SessionCompletionEvent

	for _, dir := range m.sessionDirs(now) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			filePath := filepath.Join(dir, name)
			events = append(events, m.pollFileLocked(filePath, now, initialScan)...)
		}
	}

	m.cleanupStaleLocked(now)
	m.started = true
	m.mu.Unlock()

	for _, evt := range events {
		if m.onCompletion != nil {
			m.onCompletion(evt)
		}
	}
}

func (m *SessionCompletionMonitor) sessionDirs(now time.Time) []string {
	dirs := make([]string, 0, 8)
	for daysAgo := 0; daysAgo <= 7; daysAgo++ {
		day := now.AddDate(0, 0, -daysAgo)
		dirs = append(dirs, filepath.Join(
			m.root,
			day.Format("2006"),
			day.Format("01"),
			day.Format("02"),
		))
	}
	return dirs
}

func (m *SessionCompletionMonitor) pollFileLocked(filePath string, now time.Time, initialScan bool) []SessionCompletionEvent {
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil
	}

	tracked := m.tracked[filePath]
	isNewFile := tracked == nil
	if isNewFile {
		if now.Sub(stat.ModTime()) > trackFreshFileWindow {
			return nil
		}
		tracked = &trackedCompletionFile{lastEventTime: now}
		m.tracked[filePath] = tracked
	}

	if stat.Size() <= tracked.offset {
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	readLen := stat.Size() - tracked.offset
	buf := make([]byte, readLen)
	if _, err := file.ReadAt(buf, tracked.offset); err != nil {
		return nil
	}
	tracked.offset = stat.Size()
	tracked.lastEventTime = now

	text := tracked.partial + string(buf)
	lines := strings.Split(text, "\n")
	tracked.partial = lines[len(lines)-1]
	lines = lines[:len(lines)-1]

	var events []SessionCompletionEvent
	emitDuringScan := !initialScan && !isNewFile
	for _, line := range lines {
		if evt, ok := m.processLineLocked(tracked, line, emitDuringScan); ok {
			events = append(events, evt)
		}
	}

	if !initialScan && isNewFile {
		if evt, ok := m.buildInitialCompletionEventLocked(tracked); ok {
			events = append(events, evt)
		}
	}

	return events
}

func (m *SessionCompletionMonitor) processLineLocked(tracked *trackedCompletionFile, line string, emitCompletion bool) (SessionCompletionEvent, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return SessionCompletionEvent{}, false
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return SessionCompletionEvent{}, false
	}

	lineType, _ := obj["type"].(string)
	switch lineType {
	case "session_meta":
		payload, _ := obj["payload"].(map[string]interface{})
		if tracked.sessionID == "" {
			if sessionID, _ := payload["id"].(string); sessionID != "" {
				tracked.sessionID = sessionID
				tracked.notifiedCompletionCount = maxInt(tracked.notifiedCompletionCount, m.sessionNotifiedCount[sessionID])
			}
		}
		if tracked.cwd == "" {
			if cwd, _ := payload["cwd"].(string); cwd != "" {
				tracked.cwd = normalizeSessionDir(cwd)
			}
		}
	case "event_msg":
		payload, _ := obj["payload"].(map[string]interface{})
		msgType, _ := payload["type"].(string)
		if msgType == "" {
			return SessionCompletionEvent{}, false
		}

		tracked.lastRelevantEvent = msgType
		if msgType == "user_message" && tracked.title == "" {
			if message, _ := payload["message"].(string); strings.TrimSpace(message) != "" {
				tracked.title = compactText(message, 46)
			}
		}

		if msgType == "task_complete" {
			tracked.completionCount++
			if emitCompletion && tracked.completionCount > tracked.notifiedCompletionCount {
				tracked.notifiedCompletionCount = tracked.completionCount
				m.recordSessionNotifiedLocked(tracked.sessionID, tracked.notifiedCompletionCount)
				return m.buildEvent(tracked), true
			}
		}
	}

	return SessionCompletionEvent{}, false
}

func (m *SessionCompletionMonitor) buildInitialCompletionEventLocked(tracked *trackedCompletionFile) (SessionCompletionEvent, bool) {
	if tracked.lastRelevantEvent != "task_complete" {
		return SessionCompletionEvent{}, false
	}
	if tracked.completionCount <= tracked.notifiedCompletionCount {
		return SessionCompletionEvent{}, false
	}
	tracked.notifiedCompletionCount = tracked.completionCount
	m.recordSessionNotifiedLocked(tracked.sessionID, tracked.notifiedCompletionCount)
	return m.buildEvent(tracked), true
}

func (m *SessionCompletionMonitor) buildEvent(tracked *trackedCompletionFile) SessionCompletionEvent {
	title := tracked.title
	if title == "" {
		title = defaultSessionTitle(tracked.sessionID)
	}
	return SessionCompletionEvent{
		SessionID:       tracked.sessionID,
		Cwd:             tracked.cwd,
		Title:           title,
		CompletionCount: tracked.completionCount,
	}
}

func (m *SessionCompletionMonitor) recordSessionNotifiedLocked(sessionID string, completionCount int) {
	if strings.TrimSpace(sessionID) == "" || completionCount <= 0 {
		return
	}
	if completionCount > m.sessionNotifiedCount[sessionID] {
		m.sessionNotifiedCount[sessionID] = completionCount
	}
}

func (m *SessionCompletionMonitor) cleanupStaleLocked(now time.Time) {
	for path, tracked := range m.tracked {
		if now.Sub(tracked.lastEventTime) > sessionMonitorStaleAfter {
			delete(m.tracked, path)
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
