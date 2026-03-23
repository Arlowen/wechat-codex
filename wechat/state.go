package wechat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type UserState struct {
	ActiveSessionID    string   `json:"active_session_id,omitempty"`
	ActiveCwd          string   `json:"active_cwd,omitempty"`
	LastSessionIDs     []string `json:"last_session_ids,omitempty"`
	PendingSessionPick bool     `json:"pending_session_pick,omitempty"`
}

type BotStateData struct {
	Users map[string]*UserState `json:"users"`
}

type BotState struct {
	path string
	data *BotStateData
	mu   sync.RWMutex
}

func NewBotState(dir string) *BotState {
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "state.json")
	bs := &BotState{
		path: path,
		data: &BotStateData{Users: make(map[string]*UserState)},
	}
	bs.load()
	return bs
}

func (s *BotState) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := os.ReadFile(s.path)
	if err == nil {
		json.Unmarshal(content, s.data)
	}
	if s.data.Users == nil {
		s.data.Users = make(map[string]*UserState)
	}
}

func (s *BotState) save() {
	content, _ := json.MarshalIndent(s.data, "", "  ")
	os.WriteFile(s.path, content, 0644)
}

func (s *BotState) getUser(userID string) *UserState {
	if u, ok := s.data.Users[userID]; ok {
		return u
	}
	u := &UserState{}
	s.data.Users[userID] = u
	return u
}

func (s *BotState) SetActiveSession(userID, sessionID, cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.getUser(userID)
	u.ActiveSessionID = sessionID
	u.ActiveCwd = cwd
	s.save()
}

func (s *BotState) ClearActiveSession(userID, cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.getUser(userID)
	u.ActiveSessionID = ""
	u.ActiveCwd = cwd
	s.save()
}

func (s *BotState) GetActive(userID string) (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.getUser(userID)
	return u.ActiveSessionID, u.ActiveCwd
}

func (s *BotState) SetLastSessionIDs(userID string, ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.getUser(userID)
	u.LastSessionIDs = ids
	s.save()
}

func (s *BotState) GetLastSessionIDs(userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.getUser(userID)
	return u.LastSessionIDs
}

func (s *BotState) SetPendingSessionPick(userID string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.getUser(userID)
	u.PendingSessionPick = enabled
	s.save()
}

func (s *BotState) IsPendingSessionPick(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.getUser(userID)
	return u.PendingSessionPick
}

type RunningPromptRegistry struct {
	mu           sync.Mutex
	runningCount map[string]int
	busySessions map[string]map[string]bool
}

func NewRunningPromptRegistry() *RunningPromptRegistry {
	return &RunningPromptRegistry{
		runningCount: make(map[string]int),
		busySessions: make(map[string]map[string]bool),
	}
}

func (r *RunningPromptRegistry) TryStart(userID, sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sessionID != "" {
		if sessions, ok := r.busySessions[userID]; ok {
			if sessions[sessionID] {
				return false
			}
		} else {
			r.busySessions[userID] = make(map[string]bool)
		}
		r.busySessions[userID][sessionID] = true
	}
	r.runningCount[userID]++
	return true
}

func (r *RunningPromptRegistry) Finish(userID, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.runningCount[userID]--
	if r.runningCount[userID] <= 0 {
		delete(r.runningCount, userID)
	}

	if sessionID != "" {
		if sessions, ok := r.busySessions[userID]; ok {
			delete(sessions, sessionID)
			if len(sessions) == 0 {
				delete(r.busySessions, userID)
			}
		}
	}
}

func (r *RunningPromptRegistry) Count(userID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runningCount[userID]
}
