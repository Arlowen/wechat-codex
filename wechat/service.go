package wechat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"wechat-codex/output"
)

const completionNotificationDedupWindow = 3 * time.Second

type CodexService struct {
	client            APIClient
	store             *AccountStore
	sessions          *SessionStore
	projects          ProjectLister
	state             *BotState
	codex             PromptRunner
	completionMonitor *SessionCompletionMonitor
	completionNotices *CompletionNotificationRegistry
	defaultCwd        string
	allowedUserIDs    map[string]bool
	pollTimeoutSec    int
	sendTyping        bool
	runningPrompts    *RunningPromptRegistry
	seenMessageIDs    map[string]bool
	seenMessageOrder  []string
}

func NewCodexService(
	client APIClient,
	store *AccountStore,
	sessions *SessionStore,
	projects ProjectLister,
	state *BotState,
	codex PromptRunner,
	defaultCwd string,
	allowedUserIDs []string,
	pollTimeoutSec int,
	sendTyping bool,
) *CodexService {
	allowed := make(map[string]bool)
	for _, id := range allowedUserIDs {
		allowed[strings.TrimSpace(id)] = true
	}
	if projects == nil {
		projects = NewProjectStore("")
	}
	return &CodexService{
		client:            client,
		store:             store,
		sessions:          sessions,
		projects:          projects,
		state:             state,
		codex:             codex,
		defaultCwd:        defaultCwd,
		allowedUserIDs:    allowed,
		pollTimeoutSec:    pollTimeoutSec,
		sendTyping:        sendTyping,
		runningPrompts:    NewRunningPromptRegistry(),
		completionNotices: NewCompletionNotificationRegistry(),
		seenMessageIDs:    make(map[string]bool),
	}
}

type CompletionNotificationRegistry struct {
	mu       sync.Mutex
	maxCount map[string]int
	lastSent map[string]time.Time
}

func NewCompletionNotificationRegistry() *CompletionNotificationRegistry {
	return &CompletionNotificationRegistry{
		maxCount: make(map[string]int),
		lastSent: make(map[string]time.Time),
	}
}

func (r *CompletionNotificationRegistry) TryMarkSent(sessionID string, completionCount int) bool {
	sessionID = strings.TrimSpace(sessionID)
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	if sessionID == "" {
		return true
	}

	if completionCount > 0 {
		if r.maxCount[sessionID] >= completionCount {
			return false
		}
		r.maxCount[sessionID] = completionCount
		r.lastSent[sessionID] = now
		return true
	}

	lastSent := r.lastSent[sessionID]
	if !lastSent.IsZero() && now.Sub(lastSent) <= completionNotificationDedupWindow {
		return false
	}
	r.lastSent[sessionID] = now
	return true
}

func (s *CodexService) RunForever() {
	buf := s.store.LoadGetUpdatesBuf()
	s.startCompletionMonitor()
	output.Infof("Starting WeChat webhook polling")

	for {
		resp, err := s.client.GetUpdates(buf, s.pollTimeoutSec)
		if err != nil {
			output.Warnf("wechat getupdates error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if errCode, ok := resp["errcode"].(float64); ok && int(errCode) == -14 {
			output.Warnf("WeChat session expired, clearing poll cursor and retrying")
			buf = ""
			s.store.ClearGetUpdatesBuf()
			time.Sleep(3 * time.Second)
			continue
		}

		if nextBuf, ok := resp["get_updates_buf"].(string); ok && nextBuf != "" {
			buf = nextBuf
			s.store.SaveGetUpdatesBuf(buf)
		}

		if msgs, ok := resp["msgs"].([]interface{}); ok {
			for _, m := range msgs {
				if msg, ok := m.(map[string]interface{}); ok {
					s.handleMessage(msg)
				}
			}
		}
	}
}

func extractText(itemList interface{}) string {
	list, ok := itemList.([]interface{})
	if !ok {
		return ""
	}
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			if t, ok := m["type"].(float64); ok && int(t) == 1 {
				if textItem, ok := m["text_item"].(map[string]interface{}); ok {
					if text, ok := textItem["text"].(string); ok {
						return strings.TrimSpace(text)
					}
				}
			}
		}
	}
	return ""
}

func (s *CodexService) sendText(toUserID, contextToken, text string) {
	chunks := s.chunkText(text, 3500)
	for _, chunk := range chunks {
		if _, err := s.client.SendText(toUserID, contextToken, chunk); err != nil {
			output.Warnf("wechat sendmessage failed: %v", err)
		}
	}
}

func (s *CodexService) chunkText(text string, size int) []string {
	if len(text) <= size {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(text) {
		end := start + size
		if end > len(text) {
			end = len(text)
		}
		if end < len(text) {
			splitAt := strings.LastIndex(text[start:end], "\n")
			if splitAt > 0 {
				end = start + splitAt + 1
			}
		}
		chunks = append(chunks, text[start:end])
		start = end
	}
	return chunks
}

func (s *CodexService) handleMessage(msg map[string]interface{}) {
	msgType, ok := msg["message_type"].(float64)
	if !ok || int(msgType) != 1 {
		return
	}

	fromUserID, _ := msg["from_user_id"].(string)
	contextToken, _ := msg["context_token"].(string)
	if fromUserID == "" || contextToken == "" {
		return
	}

	msgID, _ := msg["message_id"].(string)
	if msgID != "" {
		if s.seenMessageIDs[msgID] {
			return
		}
		s.seenMessageIDs[msgID] = true
		s.seenMessageOrder = append(s.seenMessageOrder, msgID)
		if len(s.seenMessageOrder) > 10000 {
			oldest := s.seenMessageOrder[0]
			s.seenMessageOrder = s.seenMessageOrder[1:]
			delete(s.seenMessageIDs, oldest)
		}
	}

	if len(s.allowedUserIDs) > 0 && !s.allowedUserIDs[fromUserID] {
		s.sendText(fromUserID, contextToken, "没有权限使用这个 bot。")
		return
	}
	s.state.SetNotifyTarget(fromUserID, contextToken)

	text := extractText(msg["item_list"])
	if text == "" {
		return
	}

	output.Infof("wechat message received: user_id=%s len=%d", fromUserID, len(text))

	if !strings.HasPrefix(text, "/") {
		if s.tryHandleQuickSessionPick(fromUserID, contextToken, text) {
			return
		}
		s.state.SetPendingSessionPick(fromUserID, false)
		s.runPrompt(fromUserID, contextToken, text)
		return
	}

	parts := strings.SplitN(text[1:], " ", 2)
	cmd := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "start", "help":
		s.sendHelp(fromUserID, contextToken)
	case "sessions":
		s.handleSessions(fromUserID, contextToken, arg)
	case "projects":
		s.handleProjects(fromUserID, contextToken, arg)
	case "project":
		s.handleProject(fromUserID, contextToken, arg)
	case "use":
		s.handleUse(fromUserID, contextToken, arg)
	case "status":
		s.handleStatus(fromUserID, contextToken)
	case "new":
		s.handleNew(fromUserID, contextToken, arg)
	case "history":
		s.handleHistory(fromUserID, contextToken, arg)
	case "ask":
		s.handleAsk(fromUserID, contextToken, arg)
	default:
		s.sendText(fromUserID, contextToken, fmt.Sprintf("未知命令: /%s\n发送 /help 查看说明。", cmd))
	}
}

func (s *CodexService) sendHelp(actorID, contextToken string) {
	help := `可用命令:
/sessions [N] - 查看最近 N 条会话（标题 + 编号）
/projects [N] - 查看 Codex 项目列表（用编号）
/project sessions <项目编号> [会话数] - 查看该项目下的会话
/use <编号|session_id> - 切换当前会话
/history [编号|session_id] [N] - 查看会话最近 N 条消息
/new [项目编号|cwd] - 进入新会话模式（下一条普通消息会新建 session）
/status - 查看当前绑定会话
/ask <内容> - 手动提问（可选）
执行 /sessions 后，可直接发送编号切换会话
后台执行时仍可发送 /use /sessions /status
直接发普通消息即可对话（会自动续聊当前 session）`
	s.sendText(actorID, contextToken, help)
}

func displayProjectDir(dir string) string {
	if dir == "" {
		return "未知项目"
	}

	clean := filepath.Clean(dir)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return clean
	}
	home = filepath.Clean(home)
	if clean == home {
		return "~"
	}

	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(clean, prefix) {
		return "~" + strings.TrimPrefix(clean, home)
	}
	return clean
}

func parseListLimit(raw string, defaultLimit, maxLimit int) (int, error) {
	limit := defaultLimit
	if strings.TrimSpace(raw) == "" {
		return limit, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if value < 1 {
		value = 1
	}
	if value > maxLimit {
		value = maxLimit
	}
	return value, nil
}

func shortSessionID(sessionID string) string {
	if len(sessionID) > 8 {
		return sessionID[:8]
	}
	return sessionID
}

func sessionCwdName(cwd string) string {
	cwdName := filepath.Base(cwd)
	if cwdName == "." || cwdName == "" {
		return cwd
	}
	return cwdName
}

func formatSessionEntries(items []SessionMeta, startIndex int) ([]string, []string) {
	lines := make([]string, 0, len(items))
	sessionIDs := make([]string, 0, len(items))
	for i, meta := range items {
		lines = append(lines, fmt.Sprintf("%d. %s | %s | %s", startIndex+i, meta.Title, shortSessionID(meta.SessionID), sessionCwdName(meta.Cwd)))
		sessionIDs = append(sessionIDs, meta.SessionID)
	}
	return lines, sessionIDs
}

func (s *CodexService) handleSessions(actorID, contextToken, arg string) {
	limit, err := parseListLimit(arg, 10, 30)
	if err != nil {
		s.sendText(actorID, contextToken, "参数错误，示例: /sessions 10")
		return
	}

	items, _ := s.sessions.ListRecent(limit)
	if len(items) == 0 {
		s.sendText(actorID, contextToken, "未找到本地会话记录。")
		return
	}

	lines := []string{"最近会话（用 /use 编号 切换）:"}
	sessionLines, sessionIDs := formatSessionEntries(items, 1)
	lines = append(lines, sessionLines...)
	lines = append(lines, "直接发送编号即可切换（例如发送: 1）")

	s.sendText(actorID, contextToken, strings.Join(lines, "\n"))
	s.state.SetLastSessionIDs(actorID, sessionIDs)
	s.state.SetPendingSessionPick(actorID, true)
}

func (s *CodexService) handleProjects(actorID, contextToken, arg string) {
	limit, err := parseListLimit(arg, 10, 30)
	if err != nil {
		s.sendText(actorID, contextToken, "参数错误，示例: /projects 10")
		return
	}

	items, err := s.projects.ListProjects(limit)
	if err != nil {
		if os.IsNotExist(err) {
			s.sendText(actorID, contextToken, "未找到 Codex 项目配置。")
			return
		}
		s.sendText(actorID, contextToken, fmt.Sprintf("读取 Codex 项目失败: %v", err))
		return
	}
	if len(items) == 0 {
		s.sendText(actorID, contextToken, "未找到可用的 Codex 项目。")
		return
	}

	lines := []string{"Codex 项目（用 /new 编号 新建会话）:"}
	for i, projectDir := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, displayProjectDir(projectDir)))
	}
	lines = append(lines, "示例: /new 1")

	s.sendText(actorID, contextToken, strings.Join(lines, "\n"))
	s.state.SetLastProjectDirs(actorID, items)
	s.state.SetPendingSessionPick(actorID, false)
}

func (s *CodexService) handleProject(actorID, contextToken, arg string) {
	tokens := strings.Fields(arg)
	if len(tokens) == 0 {
		s.sendText(actorID, contextToken, "示例: /project sessions 1 10")
		return
	}

	switch strings.ToLower(tokens[0]) {
	case "sessions":
		s.handleProjectSessions(actorID, contextToken, tokens[1:])
	default:
		s.sendText(actorID, contextToken, "示例: /project sessions 1 10")
	}
}

func (s *CodexService) resolveProjectSelector(actorID, selector string) (string, string) {
	raw := strings.TrimSpace(selector)
	if raw == "" {
		return "", "示例: /new 1 或 /project sessions 1 10"
	}
	idx, err := strconv.Atoi(raw)
	if err != nil {
		return "", "项目编号必须是数字。先执行 /projects 查看编号。"
	}
	projectDirs := s.state.GetLastProjectDirs(actorID)
	if idx <= 0 || idx > len(projectDirs) {
		return "", "编号无效。先执行 /projects，再用编号。"
	}
	return projectDirs[idx-1], ""
}

func (s *CodexService) handleProjectSessions(actorID, contextToken string, args []string) {
	if len(args) == 0 {
		s.sendText(actorID, contextToken, "示例: /project sessions 1 10")
		return
	}

	projectDir, errStr := s.resolveProjectSelector(actorID, args[0])
	if errStr != "" {
		s.sendText(actorID, contextToken, errStr)
		return
	}

	limit := 10
	if len(args) >= 2 {
		var err error
		limit, err = parseListLimit(args[1], 10, 30)
		if err != nil {
			s.sendText(actorID, contextToken, "会话数必须是数字，示例: /project sessions 1 10")
			return
		}
	}

	items, _ := s.sessions.ListRecentByCwd(projectDir, limit)
	if len(items) == 0 {
		s.sendText(actorID, contextToken, fmt.Sprintf("项目 %s 下暂无会话记录。", displayProjectDir(projectDir)))
		return
	}

	lines := []string{fmt.Sprintf("项目会话: %s（用 /use 编号 切换）:", displayProjectDir(projectDir))}
	sessionLines, sessionIDs := formatSessionEntries(items, 1)
	lines = append(lines, sessionLines...)
	lines = append(lines, "直接发送编号即可切换（例如发送: 1）")

	s.sendText(actorID, contextToken, strings.Join(lines, "\n"))
	s.state.SetLastSessionIDs(actorID, sessionIDs)
	s.state.SetPendingSessionPick(actorID, true)
}

func (s *CodexService) resolveSessionSelector(actorID, selector string) (string, string) {
	raw := strings.TrimSpace(selector)
	if raw == "" {
		return "", "示例: /use 1 或 /use <session_id>"
	}
	if idx, err := strconv.Atoi(raw); err == nil {
		recentIDs := s.state.GetLastSessionIDs(actorID)
		if idx <= 0 || idx > len(recentIDs) {
			return "", "编号无效。先执行 /sessions，再用编号。"
		}
		return recentIDs[idx-1], ""
	}
	return raw, ""
}

func (s *CodexService) switchToSession(actorID, contextToken, sessionID string) {
	meta := s.sessions.FindByID(sessionID)
	if meta == nil {
		s.sendText(actorID, contextToken, fmt.Sprintf("未找到 session: %s", sessionID))
		return
	}
	s.state.SetActiveSession(actorID, meta.SessionID, meta.Cwd)
	s.state.SetPendingSessionPick(actorID, false)
	s.sendText(actorID, contextToken, fmt.Sprintf("已切换到:\n%s\nsession: %s\ncwd: %s\n现在可直接发消息对话。", meta.Title, meta.SessionID, meta.Cwd))
}

func (s *CodexService) handleUse(actorID, contextToken, arg string) {
	sessionID, errStr := s.resolveSessionSelector(actorID, arg)
	if errStr != "" {
		s.sendText(actorID, contextToken, errStr)
		return
	}
	if sessionID == "" {
		s.sendText(actorID, contextToken, "无效的会话选择参数。")
		return
	}
	s.switchToSession(actorID, contextToken, sessionID)
}

func (s *CodexService) tryHandleQuickSessionPick(actorID, contextToken, text string) bool {
	if !s.state.IsPendingSessionPick(actorID) {
		return false
	}
	raw := strings.TrimSpace(text)
	idx, err := strconv.Atoi(raw)
	if err != nil {
		return false
	}
	recentIDs := s.state.GetLastSessionIDs(actorID)
	if idx <= 0 || idx > len(recentIDs) {
		s.sendText(actorID, contextToken, "编号无效。请发送 /sessions 重新查看列表。")
		return true
	}
	s.switchToSession(actorID, contextToken, recentIDs[idx-1])
	return true
}

func (s *CodexService) handleHistory(actorID, contextToken, arg string) {
	tokens := strings.Fields(arg)
	limit := 10
	var sessionID string

	if len(tokens) == 0 {
		activeID, _ := s.state.GetActive(actorID)
		if activeID == "" {
			s.sendText(actorID, contextToken, "当前无 active session。先 /use 选择会话，或直接对话后再查看历史。")
			return
		}
		sessionID = activeID
	} else {
		resolvedID, errStr := s.resolveSessionSelector(actorID, tokens[0])
		if errStr != "" {
			s.sendText(actorID, contextToken, errStr)
			return
		}
		if resolvedID == "" {
			s.sendText(actorID, contextToken, "无效的会话选择参数。")
			return
		}
		sessionID = resolvedID
		if len(tokens) >= 2 {
			if l, err := strconv.Atoi(tokens[1]); err == nil {
				limit = l
			} else {
				s.sendText(actorID, contextToken, "N 必须是数字，示例: /history 1 20")
				return
			}
		}
	}

	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	meta, messages := s.sessions.GetHistory(sessionID, limit)
	if meta == nil {
		s.sendText(actorID, contextToken, fmt.Sprintf("未找到 session: %s", sessionID))
		return
	}
	if len(messages) == 0 {
		s.sendText(actorID, contextToken, "该会话暂无可展示历史消息。")
		return
	}

	lines := []string{
		fmt.Sprintf("会话历史: %s", meta.Title),
		fmt.Sprintf("session: %s", meta.SessionID),
		fmt.Sprintf("显示最近 %d 条消息:", len(messages)),
	}

	for i, msg := range messages {
		roleZh := "助手"
		if msg.Role == "user" {
			roleZh = "用户"
		}
		lines = append(lines, fmt.Sprintf("%d. [%s] %s", i+1, roleZh, CompactMessage(msg.Content, 320)))
	}
	s.sendText(actorID, contextToken, strings.Join(lines, "\n"))
}

func (s *CodexService) handleStatus(actorID, contextToken string) {
	sessionID, cwd := s.state.GetActive(actorID)
	runningCount := s.runningPrompts.Count(actorID)

	if sessionID == "" {
		msg := "当前没有绑定会话。可先 /sessions + /use，或 /new 后直接发消息。"
		if runningCount > 0 {
			msg += fmt.Sprintf("\n后台仍有 %d 个任务运行，可继续 /use 切线程。", runningCount)
		}
		s.sendText(actorID, contextToken, msg)
		return
	}

	title := "session " + sessionID
	if len(title) > 16 {
		title = "session " + sessionID[:8]
	}
	meta := s.sessions.FindByID(sessionID)
	if meta != nil && meta.Title != "" {
		title = meta.Title
	}

	if cwd == "" {
		cwd = s.defaultCwd
	}

	lines := []string{
		"当前会话:",
		title,
		fmt.Sprintf("session: %s", sessionID),
		fmt.Sprintf("cwd: %s", cwd),
		"支持与本地 Codex 客户端交替续聊。",
	}
	if runningCount > 0 {
		lines = append(lines, fmt.Sprintf("后台运行中: %d 个任务（可继续 /use 切线程）", runningCount))
	}
	s.sendText(actorID, contextToken, strings.Join(lines, "\n"))
}

func (s *CodexService) handleAsk(actorID, contextToken, arg string) {
	prompt := strings.TrimSpace(arg)
	if prompt == "" {
		s.sendText(actorID, contextToken, "示例: /ask 帮我总结当前仓库结构")
		return
	}
	s.runPrompt(actorID, contextToken, prompt)
}

func (s *CodexService) handleNew(actorID, contextToken, arg string) {
	cwdRaw := strings.TrimSpace(arg)
	_, currentCwd := s.state.GetActive(actorID)
	targetCwd := currentCwd
	if targetCwd == "" {
		targetCwd = s.defaultCwd
	}

	if cwdRaw != "" {
		if _, err := strconv.Atoi(cwdRaw); err == nil {
			projectDir, errStr := s.resolveProjectSelector(actorID, cwdRaw)
			if errStr != "" {
				s.sendText(actorID, contextToken, errStr)
				return
			}
			targetCwd = projectDir
		} else {
			resolvedCwd, err := resolveExistingDir(cwdRaw)
			if err != nil {
				s.sendText(actorID, contextToken, err.Error())
				return
			}
			targetCwd = resolvedCwd
		}
	}

	s.state.ClearActiveSession(actorID, targetCwd)
	s.state.SetPendingSessionPick(actorID, false)
	s.sendText(actorID, contextToken, fmt.Sprintf("已进入新会话模式，cwd: %s\n下一条普通消息会创建一个新 session。", targetCwd))
}

func (s *CodexService) startCompletionMonitor() {
	if s.completionMonitor != nil {
		return
	}
	s.completionMonitor = NewSessionCompletionMonitor(
		s.sessions.Root,
		defaultSessionCompletionPollInterval,
		func(evt SessionCompletionEvent) {
			s.notifySessionCompletion(evt.SessionID, evt.Cwd, evt.Title, evt.CompletionCount)
		},
	)
	s.completionMonitor.Start()
}

func (s *CodexService) notifySessionCompletion(sessionID, cwd, title string, completionCount int) {
	if s.completionNotices != nil && !s.completionNotices.TryMarkSent(sessionID, completionCount) {
		if s.completionMonitor != nil && sessionID != "" && completionCount > 0 {
			s.completionMonitor.MarkNotified(sessionID, completionCount)
		}
		return
	}

	if s.completionMonitor != nil && sessionID != "" && completionCount > 0 {
		s.completionMonitor.MarkNotified(sessionID, completionCount)
	}

	userID, contextToken := s.state.GetNotifyTarget()
	if userID == "" || contextToken == "" {
		return
	}

	projectName := s.resolveNotificationProjectName(cwd)
	sessionTitle := s.resolveNotificationSessionTitle(sessionID, title)
	s.sendText(userID, contextToken, fmt.Sprintf("%s-%s-已完成本次对话。", projectName, sessionTitle))
}

func (s *CodexService) resolveNotificationProjectName(cwd string) string {
	target := normalizeSessionDir(cwd)
	if target != "" {
		projects, err := s.projects.ListProjects(0)
		if err == nil {
			var bestMatch string
			for _, projectDir := range projects {
				projectDir = normalizeProjectPath(projectDir)
				if projectDir == "" {
					continue
				}
				if target == projectDir || strings.HasPrefix(target, projectDir+string(os.PathSeparator)) {
					if len(projectDir) > len(bestMatch) {
						bestMatch = projectDir
					}
				}
			}
			if bestMatch != "" {
				if base := filepath.Base(bestMatch); base != "" && base != "." && base != string(os.PathSeparator) {
					return base
				}
				return displayProjectDir(bestMatch)
			}
		}

		if base := filepath.Base(target); base != "" && base != "." && base != string(os.PathSeparator) {
			return base
		}
	}
	return "未知项目"
}

func (s *CodexService) resolveNotificationSessionTitle(sessionID, title string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return defaultSessionTitle(sessionID)
}

func (s *CodexService) loadSessionMetaWithRetry(sessionID string, attempts int, delay time.Duration) *SessionMeta {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if attempts < 1 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		meta := s.sessions.FindByID(sessionID)
		if meta != nil {
			return meta
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	return nil
}

func (s *CodexService) runPrompt(actorID, contextToken, prompt string) {
	activeID, activeCwd := s.state.GetActive(actorID)
	cwd := activeCwd
	if cwd == "" {
		cwd = s.defaultCwd
	}
	if !dirExists(cwd) {
		cwd = s.defaultCwd
	}

	if !s.runningPrompts.TryStart(actorID, activeID) {
		busySession := "当前线程"
		if activeID != "" {
			if len(activeID) > 8 {
				busySession = activeID[:8]
			} else {
				busySession = activeID
			}
		}
		s.sendText(actorID, contextToken, fmt.Sprintf("会话 %s 已有任务运行中。可先 /use 切到其他线程，或等待当前回复完成。", busySession))
		return
	}

	go func() {
		var typing *TypingStatus
		if s.sendTyping {
			typing = NewTypingStatus(s.client, actorID, contextToken)
			typing.Start()
		}

		defer func() {
			if typing != nil {
				typing.Stop()
			}
			s.runningPrompts.Finish(actorID, activeID)
		}()

		res, err := s.codex.RunPrompt(context.Background(), prompt, cwd, activeID, nil)
		if err != nil {
			stderrText := ""
			if res != nil {
				stderrText = res.StderrText
			}
			message := fmt.Sprintf("调用 Codex 时出现异常: %v", err)
			if stderrText != "" {
				message += fmt.Sprintf("\n\nstderr:\n%s", tailText(stderrText, 1200))
			}
			s.sendText(actorID, contextToken, message)
			return
		}

		if res == nil {
			s.sendText(actorID, contextToken, "Codex 没有返回可展示内容。")
			return
		}

		sessionUpdated := false
		if res.ThreadID != "" {
			sessionUpdated = s.state.UpdateActiveSessionIfUnchanged(actorID, activeID, res.ThreadID, cwd)
		}

		if res.ReturnCode != 0 {
			message := fmt.Sprintf("Codex 执行失败 (exit=%d)\n%s", res.ReturnCode, res.AgentText)
			if res.StderrText != "" {
				message += fmt.Sprintf("\n\nstderr:\n%s", tailText(res.StderrText, 1200))
			}
			s.sendText(actorID, contextToken, message)
			return
		}

		answer := strings.TrimSpace(res.AgentText)
		if answer == "" {
			answer = "Codex 没有返回可展示内容。"
		}

		if res.ThreadID != "" && !sessionUpdated {
			currentActiveID, _ := s.state.GetActive(actorID)
			if currentActiveID != res.ThreadID {
				note := "当前活动线程未变；这是后台线程的回复。"
				if activeID == "" {
					note = "新线程已创建，但你已经切到别的线程，当前活动线程未变。"
				}
				answer = note + "\n\n" + answer
			}
		}

		s.sendText(actorID, contextToken, answer)

		notifySessionID := res.ThreadID
		if notifySessionID == "" {
			notifySessionID = activeID
		}
		notifyCwd := cwd
		notifyTitle := ""
		notifyCompletionCount := 0
		if meta := s.loadSessionMetaWithRetry(notifySessionID, 5, 150*time.Millisecond); meta != nil {
			if meta.Cwd != "" {
				notifyCwd = meta.Cwd
			}
			notifyTitle = meta.Title
			notifyCompletionCount = meta.CompletedTurns
		}
		s.notifySessionCompletion(notifySessionID, notifyCwd, notifyTitle, notifyCompletionCount)
	}()
}

func expandUserPath(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	if value == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return value
}

func resolveExistingDir(raw string) (string, error) {
	target := expandUserPath(raw)
	if target == "" {
		return "", fmt.Errorf("cwd 不存在或不是目录: %s", strings.TrimSpace(raw))
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	target = filepath.Clean(target)

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("cwd 不存在或不是目录: %s", target)
	}
	return target, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func tailText(text string, limit int) string {
	value := strings.TrimSpace(text)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
}
