package wechat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type CodexRunner struct {
	BinPath              string
	SandboxMode          string
	ApprovalPolicy       string
	DangerousBypassLevel int
	IdleTimeout          time.Duration
}

func NewCodexRunner(binPath string) *CodexRunner {
	return &CodexRunner{
		BinPath:     binPath,
		IdleTimeout: 1 * time.Hour,
	}
}

type RunResult struct {
	ThreadID   string
	AgentText  string
	StderrText string
	ReturnCode int
}

type PromptRunner interface {
	RunPrompt(ctx context.Context, prompt, cwd string, sessionID string, onUpdate func(string)) (*RunResult, error)
}

func extractTextFragment(node interface{}) string {
	if node == nil {
		return ""
	}
	switch v := node.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, el := range v {
			sb.WriteString(extractTextFragment(el))
		}
		return sb.String()
	case map[string]interface{}:
		for _, key := range []string{"text", "delta", "text_delta", "content", "message", "output_text"} {
			if val, ok := v[key]; ok {
				extracted := extractTextFragment(val)
				if extracted != "" {
					return extracted
				}
			}
		}
		var sb strings.Builder
		for _, val := range v {
			sb.WriteString(extractTextFragment(val))
		}
		return sb.String()
	}
	return ""
}

func (r *CodexRunner) RunPrompt(ctx context.Context, prompt, cwd string, sessionID string, onUpdate func(string)) (*RunResult, error) {
	args := []string{"exec"}

	if sessionID != "" {
		args = append(args, "resume")
	}

	if r.DangerousBypassLevel == 1 {
		sm := r.SandboxMode
		if sm == "" {
			sm = "danger-full-access"
		}
		ap := r.ApprovalPolicy
		if ap == "" {
			ap = "never"
		}
		args = append(args, "-c", fmt.Sprintf("sandbox_mode=\"%s\"", sm))
		args = append(args, "-c", fmt.Sprintf("approval_policy=\"%s\"", ap))
	}

	args = append(args, "--json", "--skip-git-repo-check")
	if r.DangerousBypassLevel >= 2 {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}

	if sessionID != "" {
		args = append(args, sessionID)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, r.BinPath, args...)
	cmd.Dir = cwd

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		returnCode := 1
		var execErr *exec.Error
		if errors.As(err, &execErr) || errors.Is(err, exec.ErrNotFound) || os.IsNotExist(err) {
			returnCode = 127
		}
		return &RunResult{
			StderrText: err.Error(),
			ReturnCode: returnCode,
		}, err
	}

	var stderrBuilder strings.Builder
	var stderrMu sync.Mutex

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			stderrMu.Lock()
			stderrBuilder.WriteString(scanner.Text() + "\n")
			stderrMu.Unlock()
		}
	}()

	var threadID string
	var messages []string
	var currentAgentText string
	var lastEmitted string

	scanner := bufio.NewScanner(stdoutPipe)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var lastOutput time.Time
	var outputMu sync.Mutex

	markOutput := func() {
		outputMu.Lock()
		lastOutput = time.Now()
		outputMu.Unlock()
	}
	markOutput()

	watchdogDone := make(chan struct{})
	if r.IdleTimeout > 0 {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-watchdogDone:
					return
				case <-ticker.C:
					outputMu.Lock()
					idle := time.Since(lastOutput)
					outputMu.Unlock()
					if idle > r.IdleTimeout {
						cmd.Process.Kill()
						return
					}
				}
			}
		}()
	}

	for scanner.Scan() {
		markOutput()
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		evtType, _ := evt["type"].(string)
		evtType = strings.ToLower(evtType)

		if evtType == "thread.started" {
			if tID, _ := evt["thread_id"].(string); tID != "" {
				threadID = tID
			} else if thread, ok := evt["thread"].(map[string]interface{}); ok {
				if tID, _ := thread["id"].(string); tID != "" {
					threadID = tID
				}
			}
		}

		item, _ := evt["item"].(map[string]interface{})
		itemType, _ := item["type"].(string)
		itemType = strings.ToLower(itemType)
		isAgentItem := itemType == "agent_message" || itemType == "assistant_message"

		changed := false

		switch evtType {
		case "item.delta", "response.output_text.delta", "assistant_message.delta", "message.delta":
			delta := extractTextFragment(evt["delta"])
			if delta == "" {
				delta = extractTextFragment(evt["text_delta"])
			}
			if delta == "" {
				delta = extractTextFragment(evt["text"])
			}
			if delta == "" && item != nil {
				delta = extractTextFragment(item["delta"])
			}
			if delta == "" && item != nil {
				delta = extractTextFragment(item["text_delta"])
			}
			if delta != "" {
				if currentAgentText == "" {
					currentAgentText = delta
				} else if strings.HasPrefix(delta, currentAgentText) {
					currentAgentText = delta
				} else if !strings.HasSuffix(currentAgentText, delta) {
					currentAgentText += delta
				}
				changed = true
			}

		case "item.updated", "item.completed":
			if isAgentItem {
				fullText := extractTextFragment(item["text"])
				if fullText == "" {
					fullText = extractTextFragment(item["content"])
				}
				if fullText == "" {
					fullText = extractTextFragment(item["message"])
				}
				fullText = strings.TrimSpace(fullText)
				if fullText != "" {
					currentAgentText = fullText
					changed = true
				}
				if evtType == "item.completed" && currentAgentText != "" {
					messages = append(messages, currentAgentText)
					changed = true
					currentAgentText = ""
				}
			}

		case "turn.completed", "response.completed", "thread.completed":
			fallbackText := extractTextFragment(evt["output_text"])
			if fallbackText == "" {
				fallbackText = extractTextFragment(evt["text"])
			}
			fallbackText = strings.TrimSpace(fallbackText)
			if fallbackText != "" && (len(messages) == 0 || messages[len(messages)-1] != fallbackText) {
				messages = append(messages, fallbackText)
				changed = true
			}
			if currentAgentText != "" {
				messages = append(messages, strings.TrimSpace(currentAgentText))
				changed = true
				currentAgentText = ""
			}
		}

		if onUpdate != nil && changed {
			var liveParts []string
			for _, m := range messages {
				if m != "" {
					liveParts = append(liveParts, m)
				}
			}
			if currentAgentText != "" {
				liveParts = append(liveParts, currentAgentText)
			}
			liveText := strings.TrimSpace(strings.Join(liveParts, "\n\n"))
			if liveText != "" && liveText != lastEmitted {
				onUpdate(liveText)
				lastEmitted = liveText
			}
		}
	}

	err = cmd.Wait()
	close(watchdogDone)

	stderrMu.Lock()
	stderrText := strings.TrimSpace(stderrBuilder.String())
	stderrMu.Unlock()

	returnCode := 0
	if exitError, ok := err.(*exec.ExitError); ok {
		returnCode = exitError.ExitCode()
	} else if err != nil {
		returnCode = 1
	}

	if currentAgentText != "" {
		messages = append(messages, strings.TrimSpace(currentAgentText))
	}

	var finalParts []string
	for _, m := range messages {
		if m != "" {
			finalParts = append(finalParts, m)
		}
	}
	agentText := strings.TrimSpace(strings.Join(finalParts, "\n\n"))

	if agentText == "" {
		agentText = "Codex 没有返回可展示内容。"
	}

	res := &RunResult{
		ThreadID:   threadID,
		AgentText:  agentText,
		StderrText: stderrText,
		ReturnCode: returnCode,
	}

	return res, nil
}
