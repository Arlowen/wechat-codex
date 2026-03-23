package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeSessionFile(t *testing.T, root, sessionID, cwd, titlePrompt string) {
	t.Helper()

	dayDir := filepath.Join(root, "2026", "03", "22")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	payloads := []map[string]interface{}{
		{
			"type": "session_meta",
			"payload": map[string]interface{}{
				"id":        sessionID,
				"timestamp": "2026-03-22T00:00:00Z",
				"cwd":       cwd,
			},
		},
		{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type":    "user_message",
				"message": titlePrompt,
			},
		},
		{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type":    "agent_message",
				"message": "done",
			},
		},
	}

	var lines []string
	for _, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		lines = append(lines, string(raw))
	}

	target := filepath.Join(dayDir, sessionID+".jsonl")
	if err := os.WriteFile(target, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
}

type fakeClient struct {
	mu   sync.Mutex
	sent []string
}

func (c *fakeClient) GetUpdates(buf string, timeoutSec int) (map[string]interface{}, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeClient) SendText(toUserID, contextToken, text string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, fmt.Sprintf("%s|%s|%s", toUserID, contextToken, text))
	return fmt.Sprintf("mid-%d", len(c.sent)), nil
}

func (c *fakeClient) GetConfig(ilinkUserID, contextToken string) (map[string]interface{}, error) {
	return map[string]interface{}{"typing_ticket": "ticket-1"}, nil
}

func (c *fakeClient) SendTyping(ilinkUserID, typingTicket string, status int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, fmt.Sprintf("%s|typing|%d", ilinkUserID, status))
	return nil
}

func (c *fakeClient) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	run   func(context.Context, string, string, string, func(string)) (*RunResult, error)
}

func (r *fakeRunner) RunPrompt(ctx context.Context, prompt, cwd, sessionID string, onUpdate func(string)) (*RunResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, fmt.Sprintf("%s|%s|%s", prompt, cwd, sessionID))
	r.mu.Unlock()

	if r.run != nil {
		return r.run(ctx, prompt, cwd, sessionID, onUpdate)
	}
	return &RunResult{
		ThreadID:   "thread-123",
		AgentText:  "answer:" + prompt,
		ReturnCode: 0,
	}, nil
}

func (r *fakeRunner) recordedCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

func newTestService(t *testing.T, client APIClient, runner PromptRunner, sessionsRoot string) (*CodexService, *BotState) {
	t.Helper()

	runtimeDir := t.TempDir()
	state := NewBotState(runtimeDir)
	service := NewCodexService(
		client,
		NewAccountStore(runtimeDir),
		NewSessionStore(sessionsRoot),
		state,
		runner,
		runtimeDir,
		[]string{"user@im.wechat"},
		35,
		false,
	)
	return service, state
}

func TestExtractTextFromItemList(t *testing.T) {
	text := extractText([]interface{}{
		map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "  hi  "}},
		map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "ignored"}},
	})
	if text != "hi" {
		t.Fatalf("unexpected extracted text: %q", text)
	}
}

func TestAccountStorePersistsTokenAndBuf(t *testing.T) {
	store := NewAccountStore(t.TempDir())

	if err := store.SaveAccount(Account{Token: "abc", BaseURL: "https://example.com"}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := store.SaveGetUpdatesBuf("buf-1"); err != nil {
		t.Fatalf("save getupdates buf: %v", err)
	}

	account, err := store.LoadAccount()
	if err != nil {
		t.Fatalf("load account: %v", err)
	}
	if account.Token != "abc" {
		t.Fatalf("unexpected token: %q", account.Token)
	}
	if got := store.LoadGetUpdatesBuf(); got != "buf-1" {
		t.Fatalf("unexpected getupdates buf: %q", got)
	}
}

func TestCommandDispatchAndSessionPick(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	writeSessionFile(t, sessionsRoot, "sess-1", root, "first prompt")

	client := &fakeClient{}
	runner := &fakeRunner{}
	service, state := newTestService(t, client, runner, sessionsRoot)

	service.handleMessage(map[string]interface{}{
		"message_type":  float64(1),
		"message_id":    "1",
		"from_user_id":  "user@im.wechat",
		"context_token": "ctx-1",
		"item_list": []interface{}{
			map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "/sessions"}},
		},
	})

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "最近会话")
	})

	service.handleMessage(map[string]interface{}{
		"message_type":  float64(1),
		"message_id":    "2",
		"from_user_id":  "user@im.wechat",
		"context_token": "ctx-2",
		"item_list": []interface{}{
			map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "1"}},
		},
	})

	activeID, _ := state.GetActive("user@im.wechat")
	if activeID != "sess-1" {
		t.Fatalf("unexpected active session: %q", activeID)
	}

	service.handleMessage(map[string]interface{}{
		"message_type":  float64(1),
		"message_id":    "3",
		"from_user_id":  "user@im.wechat",
		"context_token": "ctx-3",
		"item_list": []interface{}{
			map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "继续这个会话"}},
		},
	})

	waitFor(t, time.Second, func() bool {
		return len(runner.recordedCalls()) == 1
	})
}

func TestUnauthorizedUserIsRejected(t *testing.T) {
	client := &fakeClient{}
	runner := &fakeRunner{}
	service, _ := newTestService(t, client, runner, t.TempDir())

	service.handleMessage(map[string]interface{}{
		"message_type":  float64(1),
		"message_id":    "1",
		"from_user_id":  "other@im.wechat",
		"context_token": "ctx-1",
		"item_list": []interface{}{
			map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "hello"}},
		},
	})

	messages := strings.Join(client.messages(), "\n")
	if !strings.Contains(messages, "没有权限使用这个 bot") {
		t.Fatalf("expected unauthorized response, got %q", messages)
	}
	if len(runner.recordedCalls()) != 0 {
		t.Fatalf("runner should not be called for unauthorized user: %#v", runner.recordedCalls())
	}
}

func TestHandleNewRejectsInvalidCwd(t *testing.T) {
	client := &fakeClient{}
	runner := &fakeRunner{}
	service, _ := newTestService(t, client, runner, t.TempDir())

	service.handleNew("user@im.wechat", "ctx-new", "/path/that/does/not/exist")

	messages := strings.Join(client.messages(), "\n")
	if !strings.Contains(messages, "cwd 不存在或不是目录") {
		t.Fatalf("expected invalid cwd message, got %q", messages)
	}
}

func TestPromptWorkerSendsFinalAnswer(t *testing.T) {
	client := &fakeClient{}
	runner := &fakeRunner{}
	service, state := newTestService(t, client, runner, t.TempDir())

	service.runPrompt("user@im.wechat", "ctx-final", "hello")

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "answer:hello")
	})

	activeID, _ := state.GetActive("user@im.wechat")
	if activeID != "thread-123" {
		t.Fatalf("unexpected active session after prompt: %q", activeID)
	}
}

func TestPromptWorkerKeepsSwitchedSession(t *testing.T) {
	client := &fakeClient{}
	release := make(chan struct{})
	runner := &fakeRunner{
		run: func(ctx context.Context, prompt, cwd, sessionID string, onUpdate func(string)) (*RunResult, error) {
			<-release
			return &RunResult{
				ThreadID:   "thread-new",
				AgentText:  "answer:hello",
				ReturnCode: 0,
			}, nil
		},
	}
	service, state := newTestService(t, client, runner, t.TempDir())

	service.runPrompt("user@im.wechat", "ctx-final", "hello")

	waitFor(t, time.Second, func() bool {
		return len(runner.recordedCalls()) == 1
	})
	state.SetActiveSession("user@im.wechat", "sess-other", t.TempDir())
	close(release)

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "answer:hello")
	})

	activeID, _ := state.GetActive("user@im.wechat")
	if activeID != "sess-other" {
		t.Fatalf("active session was overwritten: %q", activeID)
	}

	messages := strings.Join(client.messages(), "\n")
	if !strings.Contains(messages, "新线程已创建，但你已经切到别的线程") {
		t.Fatalf("expected switched-session note, got %q", messages)
	}
}

func TestRunPromptReportsRunnerError(t *testing.T) {
	client := &fakeClient{}
	runner := &fakeRunner{
		run: func(ctx context.Context, prompt, cwd, sessionID string, onUpdate func(string)) (*RunResult, error) {
			return nil, errors.New("boom")
		},
	}
	service, _ := newTestService(t, client, runner, t.TempDir())

	service.runPrompt("user@im.wechat", "ctx-error", "hello")

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "调用 Codex 时出现异常: boom")
	})
}

func TestRunPromptReportsNonZeroExit(t *testing.T) {
	client := &fakeClient{}
	runner := &fakeRunner{
		run: func(ctx context.Context, prompt, cwd, sessionID string, onUpdate func(string)) (*RunResult, error) {
			return &RunResult{
				ThreadID:   "thread-123",
				AgentText:  "partial output",
				StderrText: "stderr output",
				ReturnCode: 23,
			}, nil
		},
	}
	service, _ := newTestService(t, client, runner, t.TempDir())

	service.runPrompt("user@im.wechat", "ctx-exit", "hello")

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "Codex 执行失败 (exit=23)")
	})

	messages := strings.Join(client.messages(), "\n")
	if !strings.Contains(messages, "stderr output") {
		t.Fatalf("expected stderr in failure response, got %q", messages)
	}
}
