package wechat

import (
	"context"
	"testing"
)

func TestRunPromptReportsMissingCodex(t *testing.T) {
	runner := NewCodexRunner("/path/that/does/not/exist/codex")

	result, err := runner.RunPrompt(context.Background(), "hello", t.TempDir(), "", nil)
	if err == nil {
		t.Fatal("expected RunPrompt to return an error for missing codex binary")
	}
	if result == nil {
		t.Fatal("expected RunPrompt to return a result alongside the error")
	}
	if result.ReturnCode != 127 {
		t.Fatalf("unexpected return code: got %d want 127", result.ReturnCode)
	}
}
