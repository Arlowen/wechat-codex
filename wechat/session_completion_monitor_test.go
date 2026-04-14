package wechat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeRolloutSessionFile(t *testing.T, root string, now time.Time, fileSuffix, sessionID, cwd string, events ...map[string]interface{}) string {
	t.Helper()

	dayDir := filepath.Join(root, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	payloads := []map[string]interface{}{
		{
			"type": "session_meta",
			"payload": map[string]interface{}{
				"id":        sessionID,
				"timestamp": now.Format(time.RFC3339),
				"cwd":       cwd,
			},
		},
	}
	payloads = append(payloads, events...)

	target := filepath.Join(dayDir, fmt.Sprintf("rollout-%s-%s.jsonl", now.Format("2006-01-02T15-04-05"), fileSuffix))
	writeJSONLLines(t, target, payloads...)
	setFileModTime(t, target, now)
	return target
}

func writeJSONLLines(t *testing.T, path string, payloads ...map[string]interface{}) {
	t.Helper()

	lines := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		lines = append(lines, string(raw))
	}
	if err := os.WriteFile(path, []byte(stringsJoinLines(lines)), 0o644); err != nil {
		t.Fatalf("write jsonl file: %v", err)
	}
}

func appendJSONLLines(t *testing.T, path string, payloads ...map[string]interface{}) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open jsonl for append: %v", err)
	}
	defer file.Close()

	for _, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if _, err := file.WriteString(string(raw) + "\n"); err != nil {
			t.Fatalf("append jsonl line: %v", err)
		}
	}
}

func stringsJoinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("%s\n", joinLines(lines))
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

func TestSessionCompletionMonitorNotifiesOnceForTrackedFileAppend(t *testing.T) {
	now := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	path := writeRolloutSessionFile(
		t,
		root,
		now,
		"tracked-file",
		"sess-append",
		filepath.Join(root, "project-alpha"),
		map[string]interface{}{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type":    "user_message",
				"message": "append complete",
			},
		},
	)

	var events []SessionCompletionEvent
	monitor := NewSessionCompletionMonitor(root, time.Second, func(evt SessionCompletionEvent) {
		events = append(events, evt)
	})
	monitor.now = func() time.Time { return now }

	monitor.poll()
	appendJSONLLines(t, path, map[string]interface{}{
		"type": "event_msg",
		"payload": map[string]interface{}{
			"type": "task_complete",
		},
	})
	setFileModTime(t, path, now)
	monitor.poll()
	monitor.poll()

	if len(events) != 1 {
		t.Fatalf("expected exactly one completion event, got %#v", events)
	}
	if events[0].Title != "append complete" {
		t.Fatalf("unexpected event title: %#v", events[0])
	}
}

func TestSessionCompletionMonitorSkipsCompletedSessionsOnStartup(t *testing.T) {
	now := time.Date(2026, time.April, 14, 11, 0, 0, 0, time.UTC)
	root := t.TempDir()
	writeRolloutSessionFile(
		t,
		root,
		now,
		"startup-complete",
		"sess-startup",
		filepath.Join(root, "project-beta"),
		map[string]interface{}{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type":    "user_message",
				"message": "startup complete",
			},
		},
		map[string]interface{}{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type": "task_complete",
			},
		},
	)

	var events []SessionCompletionEvent
	monitor := NewSessionCompletionMonitor(root, time.Second, func(evt SessionCompletionEvent) {
		events = append(events, evt)
	})
	monitor.now = func() time.Time { return now }

	monitor.poll()
	monitor.poll()

	if len(events) != 0 {
		t.Fatalf("expected no startup replay events, got %#v", events)
	}
}

func TestSessionCompletionMonitorBackfillsNewCompletedSessionBetweenPolls(t *testing.T) {
	now := time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()

	var events []SessionCompletionEvent
	monitor := NewSessionCompletionMonitor(root, time.Second, func(evt SessionCompletionEvent) {
		events = append(events, evt)
	})
	monitor.now = func() time.Time { return now }

	monitor.poll()
	writeRolloutSessionFile(
		t,
		root,
		now,
		"between-polls",
		"sess-between",
		filepath.Join(root, "project-gamma"),
		map[string]interface{}{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type":    "user_message",
				"message": "between polls",
			},
		},
		map[string]interface{}{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type": "task_complete",
			},
		},
	)
	monitor.poll()

	if len(events) != 1 {
		t.Fatalf("expected one backfilled completion event, got %#v", events)
	}
	if events[0].SessionID != "sess-between" || events[0].CompletionCount != 1 {
		t.Fatalf("unexpected completion event: %#v", events[0])
	}
}
