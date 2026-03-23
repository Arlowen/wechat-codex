package wechat

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type CodexService struct {
	client *Client
	store  *AccountStore
}

func NewCodexService(client *Client, store *AccountStore) *CodexService {
	return &CodexService{
		client: client,
		store:  store,
	}
}

func (s *CodexService) RunForever() {
	buf := s.store.LoadGetUpdatesBuf()
	log.Println("[info] Starting WeChat webhook polling...")

	for {
		resp, err := s.client.GetUpdates(buf, 35)
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

func (s *CodexService) handleMessage(msg map[string]interface{}) {
	msgType, ok := msg["message_type"].(float64)
	if !ok || int(msgType) != 1 { // MESSAGE_TYPE_USER
		return
	}

	fromUserID, _ := msg["from_user_id"].(string)
	contextToken, _ := msg["context_token"].(string)
	if fromUserID == "" || contextToken == "" {
		return
	}

	text := extractText(msg["item_list"])
	if text == "" {
		return
	}

	log.Printf("wechat message received: user_id=%s text=%q\n", fromUserID, text)

	// Stub: just echo back a simple response for now
	reply := fmt.Sprintf("收到消息: %s\n(Go 版本 wechat-codex 存根回复)", text)
	s.client.SendText(fromUserID, contextToken, reply)
}
