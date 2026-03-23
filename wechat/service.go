package wechat

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CodexService struct {
	client           *Client
	store            *AccountStore
	sessions         *SessionStore
	state            *BotState
	codex            *CodexRunner
	defaultCwd       string
	allowedUserIDs   map[string]bool
	pollTimeoutSec   int
	sendTyping       bool
	runningPrompts   *RunningPromptRegistry
	seenMessageIDs   map[string]bool
	seenMessageOrder []string
}

func NewCodexService(
	client *Client,
	store *AccountStore,
	sessions *SessionStore,
	state *BotState,
	codex *CodexRunner,
	defaultCwd string,
	allowedUserIDs []string,
	pollTimeoutSec int,
	sendTyping bool,
) *CodexService {
	allowed := make(map[string]bool)
	for _, id := range allowedUserIDs {
		allowed[strings.TrimSpace(id)] = true
	}
	return &CodexService{
		client:         client,
		store:          store,
		sessions:       sessions,
		state:          state,
		codex:          codex,
		defaultCwd:     defaultCwd,
		allowedUserIDs: allowed,
		pollTimeoutSec: pollTimeoutSec,
		sendTyping:     sendTyping,
		runningPrompts: NewRunningPromptRegistry(),
		seenMessageIDs: make(map[string]bool),
	}
}

func (s *CodexService) RunForever() {
	buf := s.store.LoadGetUpdatesBuf()
	log.Println("[info] Starting WeChat webhook polling...")

	for {
		resp, err := s.client.GetUpdates(buf, s.pollTimeoutSec)
		if err != nil {
			log.Printf("[warn] wechat getupdates error: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if errCode, ok := resp["errcode"].(float64); ok && int(errCode) == -14 {
			log.Println("[warn] WeChat session expired, clearing poll cursor and retrying")
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
		s.client.SendText(toUserID, contextToken, chunk)
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

	text := extractText(msg["item_list"])
	if text == "" {
		return
	}

	log.Printf("wechat message received: user_id=%s len=%d\n", fromUserID, len(text))

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
/use <编号|session_id> - 切换当前会话
/history [编号|session_id] [N] - 查看会话最近 N 条消息
/new [cwd] - 进入新会话模式（下一条普通消息会新建 session）
/status - 查看当前绑定会话
/ask <内容> - 手动提问（可选）
执行 /sessions 后，可直接发送编号切换会话
后台执行时仍可发送 /use /sessions /status
直接发普通消息即可对话（会自动续聊当前 session）`
	s.sendText(actorID, contextToken, help)
}

func (s *CodexService) handleSessions(actorID, contextToken, arg string) {
	limit := 10
	if arg != "" {
		if l, err := strconv.Atoi(arg); err == nil {
			if l < 1 {
				l = 1
			}
			if l > 30 {
				l = 30
			}
			limit = l
		} else {
			s.sendText(actorID, contextToken, "参数错误，示例: /sessions 10")
			return
		}
	}

	items, _ := s.sessions.ListRecent(limit)
	if len(items) == 0 {
		s.sendText(actorID, contextToken, "未找到本地会话记录。")
		return
	}

	var lines []string
	lines = append(lines, "最近会话（用 /use 编号 切换）:")
	var sessionIDs []string
	for i, meta := range items {
		shortID := meta.SessionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		cwdName := filepath.Base(meta.Cwd)
		if cwdName == "." || cwdName == "" {
			cwdName = meta.Cwd
		}
		lines = append(lines, fmt.Sprintf("%d. %s | %s | %s", i+1, meta.Title, shortID, cwdName))
		sessionIDs = append(sessionIDs, meta.SessionID)
	}
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
		targetCwd = cwdRaw
	}

	s.state.ClearActiveSession(actorID, targetCwd)
	s.state.SetPendingSessionPick(actorID, false)
	s.sendText(actorID, contextToken, fmt.Sprintf("已进入新会话模式，cwd: %s\n下一条普通消息会创建一个新 session。", targetCwd))
}

func (s *CodexService) runPrompt(actorID, contextToken, prompt string) {
	activeID, activeCwd := s.state.GetActive(actorID)
	cwd := activeCwd
	if cwd == "" {
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

		// Real prompt connection:
		res, err := s.codex.RunPrompt(context.Background(), prompt, cwd, activeID, nil)
		if err != nil {
			s.sendText(actorID, contextToken, fmt.Sprintf("调用 Codex 时出现异常: %v\nstderr:%s", err, res.StderrText))
			return
		}

		if res.ThreadID != "" && activeID == "" {
			s.state.SetActiveSession(actorID, res.ThreadID, cwd)
		}

		s.sendText(actorID, contextToken, res.AgentText)
	}()
}
