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

func writeSessionFile(t *testing.T, root, sessionID, cwd, titlePrompt string) string {
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
		{
			"type": "event_msg",
			"payload": map[string]interface{}{
				"type": "task_complete",
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
	return target
}

func setFileModTime(t *testing.T, path string, ts time.Time) {
	t.Helper()
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func lastMessageText(t *testing.T, client *fakeClient) string {
	t.Helper()
	messages := client.messages()
	if len(messages) == 0 {
		t.Fatal("expected at least one message")
	}
	parts := strings.SplitN(messages[len(messages)-1], "|", 3)
	if len(parts) != 3 {
		t.Fatalf("unexpected message format: %q", messages[len(messages)-1])
	}
	return parts[2]
}

func messageTexts(t *testing.T, client *fakeClient) []string {
	t.Helper()

	messages := client.messages()
	texts := make([]string, 0, len(messages))
	for _, raw := range messages {
		parts := strings.SplitN(raw, "|", 3)
		if len(parts) != 3 {
			t.Fatalf("unexpected message format: %q", raw)
		}
		texts = append(texts, parts[2])
	}
	return texts
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

type fakeProjectStore struct {
	projects []string
	err      error
}

func (s *fakeProjectStore) ListProjects(limit int) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	items := append([]string(nil), s.projects...)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
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
	return newTestServiceWithProjects(t, client, runner, sessionsRoot, &fakeProjectStore{})
}

func newTestServiceWithProjects(t *testing.T, client APIClient, runner PromptRunner, sessionsRoot string, projects ProjectLister) (*CodexService, *BotState) {
	t.Helper()

	runtimeDir := t.TempDir()
	state := NewBotState(runtimeDir)
	service := NewCodexService(
		client,
		NewAccountStore(runtimeDir),
		NewSessionStore(sessionsRoot),
		projects,
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
	path := writeSessionFile(t, sessionsRoot, "sess-1", root, "first prompt")
	setFileModTime(t, path, time.Date(2026, time.March, 22, 12, 0, 0, 0, time.UTC))

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

	msg := lastMessageText(t, client)
	expectedLine := fmt.Sprintf("1. first prompt | sess-1 | %s", filepath.Base(root))
	if !strings.Contains(msg, "最近会话（用 /use 编号 切换）:") {
		t.Fatalf("expected flat sessions header, got %q", msg)
	}
	if !strings.Contains(msg, expectedLine) {
		t.Fatalf("expected flat session line %q in %q", expectedLine, msg)
	}

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

func TestHandleProjectsAndNewByProjectIndex(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	projectA := filepath.Join(root, "project-a")
	projectB := filepath.Join(root, "project-b")
	if err := os.MkdirAll(projectA, 0o755); err != nil {
		t.Fatalf("mkdir project a: %v", err)
	}
	if err := os.MkdirAll(projectB, 0o755); err != nil {
		t.Fatalf("mkdir project b: %v", err)
	}

	client := &fakeClient{}
	runner := &fakeRunner{}
	projectStore := &fakeProjectStore{projects: []string{projectA, projectB}}
	service, state := newTestServiceWithProjects(t, client, runner, sessionsRoot, projectStore)

	service.handleProjects("user@im.wechat", "ctx-projects", "")

	msg := lastMessageText(t, client)
	if !strings.Contains(msg, "Codex 项目（用 /new 编号 新建会话）:") {
		t.Fatalf("expected projects header, got %q", msg)
	}
	if !strings.Contains(msg, fmt.Sprintf("1. %s", displayProjectDir(projectA))) {
		t.Fatalf("expected project A in output, got %q", msg)
	}
	if !strings.Contains(msg, fmt.Sprintf("2. %s", displayProjectDir(projectB))) {
		t.Fatalf("expected project B in output, got %q", msg)
	}

	service.handleNew("user@im.wechat", "ctx-new", "2")

	activeID, activeCwd := state.GetActive("user@im.wechat")
	if activeID != "" {
		t.Fatalf("expected empty active session for new mode, got %q", activeID)
	}
	if activeCwd != projectB {
		t.Fatalf("expected active cwd %q, got %q", projectB, activeCwd)
	}
}

func TestHandleNewRejectsInvalidProjectIndex(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	client := &fakeClient{}
	runner := &fakeRunner{}
	service, _ := newTestServiceWithProjects(t, client, runner, sessionsRoot, &fakeProjectStore{})

	service.handleNew("user@im.wechat", "ctx-new", "1")

	msg := lastMessageText(t, client)
	if !strings.Contains(msg, "编号无效。先执行 /projects，再用编号。") {
		t.Fatalf("expected invalid project index message, got %q", msg)
	}
}

func TestHandleProjectSessionsUsesExactProjectMatch(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	project := filepath.Join(root, "project-a")
	projectSub := filepath.Join(project, "sub")
	otherProject := filepath.Join(root, "project-b")
	if err := os.MkdirAll(projectSub, 0o755); err != nil {
		t.Fatalf("mkdir project subdir: %v", err)
	}
	if err := os.MkdirAll(otherProject, 0o755); err != nil {
		t.Fatalf("mkdir other project: %v", err)
	}

	pathProject := writeSessionFile(t, sessionsRoot, "sess-project", project, "project prompt")
	pathSub := writeSessionFile(t, sessionsRoot, "sess-sub", projectSub, "sub prompt")
	pathOther := writeSessionFile(t, sessionsRoot, "sess-other", otherProject, "other prompt")
	baseTime := time.Date(2026, time.March, 22, 12, 0, 0, 0, time.UTC)
	setFileModTime(t, pathProject, baseTime.Add(3*time.Minute))
	setFileModTime(t, pathSub, baseTime.Add(2*time.Minute))
	setFileModTime(t, pathOther, baseTime.Add(1*time.Minute))

	client := &fakeClient{}
	runner := &fakeRunner{}
	projectStore := &fakeProjectStore{projects: []string{project, otherProject}}
	service, state := newTestServiceWithProjects(t, client, runner, sessionsRoot, projectStore)

	service.handleProjects("user@im.wechat", "ctx-projects", "")
	service.handleProject("user@im.wechat", "ctx-project-sessions", "sessions 1 10")

	msg := lastMessageText(t, client)
	if !strings.Contains(msg, fmt.Sprintf("项目会话: %s（用 /use 编号 切换）:", displayProjectDir(project))) {
		t.Fatalf("expected project sessions header, got %q", msg)
	}
	if !strings.Contains(msg, "1. project prompt | sess-pro | project-a") {
		t.Fatalf("expected exact-match project session in output, got %q", msg)
	}
	if strings.Contains(msg, "sub prompt") {
		t.Fatalf("expected subdir session to be excluded, got %q", msg)
	}

	service.handleUse("user@im.wechat", "ctx-use", "1")

	activeID, _ := state.GetActive("user@im.wechat")
	if activeID != "sess-project" {
		t.Fatalf("unexpected active session after project session selection: %q", activeID)
	}
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
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	projectDir := filepath.Join(root, "project-alpha")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	writeSessionFile(t, sessionsRoot, "thread-123", projectDir, "feature summary")

	client := &fakeClient{}
	runner := &fakeRunner{}
	service, state := newTestServiceWithProjects(t, client, runner, sessionsRoot, &fakeProjectStore{
		projects: []string{projectDir},
	})
	state.SetNotifyTarget("user@im.wechat", "ctx-final")
	service.now = func() time.Time { return time.Date(2026, time.April, 14, 9, 30, 0, 0, time.UTC) }

	service.runPrompt("user@im.wechat", "ctx-final", "hello")

	waitFor(t, time.Second, func() bool {
		return len(client.messages()) >= 2
	})

	activeID, _ := state.GetActive("user@im.wechat")
	if activeID != "thread-123" {
		t.Fatalf("unexpected active session after prompt: %q", activeID)
	}

	texts := messageTexts(t, client)
	if texts[0] != "answer:hello" {
		t.Fatalf("expected answer first, got %#v", texts)
	}
	if texts[1] != "【2026-04-14 09:30:00】-【project-alpha】-【feature summary】- 已完成本次对话" {
		t.Fatalf("expected completion notification second, got %#v", texts)
	}
}

func TestPromptWorkerKeepsSwitchedSession(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	projectDir := filepath.Join(root, "project-beta")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	writeSessionFile(t, sessionsRoot, "thread-new", projectDir, "background thread")

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
	service, state := newTestServiceWithProjects(t, client, runner, sessionsRoot, &fakeProjectStore{
		projects: []string{projectDir},
	})
	state.SetNotifyTarget("user@im.wechat", "ctx-final")
	service.now = func() time.Time { return time.Date(2026, time.April, 14, 9, 31, 0, 0, time.UTC) }

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
	if !strings.Contains(messages, "【2026-04-14 09:31:00】-【project-beta】-【background thread】- 已完成本次对话") {
		t.Fatalf("expected completion notification, got %q", messages)
	}
}

func TestRunPromptReportsRunnerError(t *testing.T) {
	client := &fakeClient{}
	runner := &fakeRunner{
		run: func(ctx context.Context, prompt, cwd, sessionID string, onUpdate func(string)) (*RunResult, error) {
			return nil, errors.New("boom")
		},
	}
	service, state := newTestService(t, client, runner, t.TempDir())
	state.SetNotifyTarget("user@im.wechat", "ctx-error")

	service.runPrompt("user@im.wechat", "ctx-error", "hello")

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "调用 Codex 时出现异常: boom")
	})

	texts := messageTexts(t, client)
	if len(texts) != 1 {
		t.Fatalf("expected only the error message, got %#v", texts)
	}
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
	service, state := newTestService(t, client, runner, t.TempDir())
	state.SetNotifyTarget("user@im.wechat", "ctx-exit")

	service.runPrompt("user@im.wechat", "ctx-exit", "hello")

	waitFor(t, time.Second, func() bool {
		return strings.Contains(strings.Join(client.messages(), "\n"), "Codex 执行失败 (exit=23)")
	})

	messages := strings.Join(client.messages(), "\n")
	if !strings.Contains(messages, "stderr output") {
		t.Fatalf("expected stderr in failure response, got %q", messages)
	}
	if strings.Contains(messages, "已完成本次对话") {
		t.Fatalf("did not expect completion notification, got %q", messages)
	}
}

func TestResolveNotificationProjectNameUsesLongestAncestor(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project-root")
	subDir := filepath.Join(projectDir, "nested", "pkg")
	otherProject := filepath.Join(root, "other-project")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.MkdirAll(otherProject, 0o755); err != nil {
		t.Fatalf("mkdir other project: %v", err)
	}

	service, _ := newTestServiceWithProjects(t, &fakeClient{}, &fakeRunner{}, filepath.Join(root, "sessions"), &fakeProjectStore{
		projects: []string{otherProject, projectDir},
	})

	if got := service.resolveNotificationProjectName(subDir); got != "project-root" {
		t.Fatalf("expected project root name, got %q", got)
	}
	if got := service.resolveNotificationProjectName(filepath.Join(root, "scratch")); got != "scratch" {
		t.Fatalf("expected cwd basename fallback, got %q", got)
	}
}

func TestNotifySessionCompletionSkipsWithoutNotifyTarget(t *testing.T) {
	client := &fakeClient{}
	service, _ := newTestService(t, client, &fakeRunner{}, t.TempDir())

	service.notifySessionCompletion("sess-1", t.TempDir(), "demo title", 1)

	if len(client.messages()) != 0 {
		t.Fatalf("expected no notifications without target, got %#v", client.messages())
	}
}

func TestNotifySessionCompletionDeduplicatesSameCompletion(t *testing.T) {
	client := &fakeClient{}
	service, state := newTestService(t, client, &fakeRunner{}, t.TempDir())
	state.SetNotifyTarget("user@im.wechat", "ctx-notify")
	service.now = func() time.Time { return time.Date(2026, time.April, 14, 9, 32, 0, 0, time.UTC) }

	service.notifySessionCompletion("sess-1", filepath.Join(t.TempDir(), "project-zeta"), "demo title", 1)
	service.notifySessionCompletion("sess-1", filepath.Join(t.TempDir(), "project-zeta"), "demo title", 1)

	texts := messageTexts(t, client)
	if len(texts) != 1 {
		t.Fatalf("expected one deduplicated notification, got %#v", texts)
	}
	if texts[0] != "【2026-04-14 09:32:00】-【project-zeta】-【demo title】- 已完成本次对话" {
		t.Fatalf("unexpected notification text, got %#v", texts)
	}
}
