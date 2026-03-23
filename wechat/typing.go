package wechat

import (
	"sync"
	"time"
)

const (
	TypingStatusTyping = 1
	TypingStatusCancel = 2
)

type TypingStatus struct {
	api          APIClient
	userID       string
	contextToken string
	interval     time.Duration
	ticket       string
	stopChan     chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	running      bool
}

func NewTypingStatus(api APIClient, userID, contextToken string) *TypingStatus {
	return &TypingStatus{
		api:          api,
		userID:       userID,
		contextToken: contextToken,
		interval:     4 * time.Second,
	}
}

func (t *TypingStatus) ensureTicket() bool {
	if t.ticket != "" {
		return true
	}
	resp, err := t.api.GetConfig(t.userID, t.contextToken)
	if err != nil {
		return false
	}
	if ticket, ok := resp["typing_ticket"].(string); ok && ticket != "" {
		t.ticket = ticket
		return true
	}
	return false
}

func (t *TypingStatus) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return
	}

	if !t.ensureTicket() {
		return
	}

	err := t.api.SendTyping(t.userID, t.ticket, TypingStatusTyping)
	if err != nil {
		return
	}

	t.running = true
	t.stopChan = make(chan struct{})
	t.wg.Add(1)

	go func() {
		defer t.wg.Done()
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()

		for {
			select {
			case <-t.stopChan:
				return
			case <-ticker.C:
				if t.ticket == "" {
					return
				}
				_ = t.api.SendTyping(t.userID, t.ticket, TypingStatusTyping)
			}
		}
	}()
}

func (t *TypingStatus) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return
	}

	close(t.stopChan)
	t.wg.Wait()
	t.running = false

	if t.ticket != "" {
		_ = t.api.SendTyping(t.userID, t.ticket, TypingStatusCancel)
	}
}
