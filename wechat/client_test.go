package wechat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendTextReturnsAPIErrorOnNegativeRet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":-2,"errmsg":"context invalid"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-1")
	_, err := client.SendText("user-1", "ctx-1", "hello")
	if err == nil {
		t.Fatal("expected sendtext to fail on ret=-2")
	}
	if !strings.Contains(err.Error(), "ret=-2") {
		t.Fatalf("expected ret code in error, got %v", err)
	}
}

func TestClientSendTextOmitsBlankContextToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		msg, ok := payload["msg"].(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected payload shape: %#v", payload)
		}
		if _, exists := msg["context_token"]; exists {
			t.Fatalf("expected blank context token to be omitted, got %#v", msg["context_token"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-1")
	if _, err := client.SendText("user-1", "", "hello"); err != nil {
		t.Fatalf("expected sendtext to succeed without context token field, got %v", err)
	}
}
