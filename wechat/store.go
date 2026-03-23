package wechat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Account struct {
	Token     string `json:"token"`
	AccountID string `json:"account_id"`
	UserID    string `json:"user_id"`
	BaseURL   string `json:"base_url"`
	SavedAt   string `json:"saved_at"`
}

type PollState struct {
	GetUpdatesBuf string `json:"get_updates_buf"`
	UpdatedAt     string `json:"updated_at"`
}

type AccountStore struct {
	runtimeDir   string
	accountPath  string
	pollStatus   string
	mu           sync.RWMutex
}

func NewAccountStore(runtimeDir string) *AccountStore {
	os.MkdirAll(runtimeDir, 0755)
	return &AccountStore{
		runtimeDir:  runtimeDir,
		accountPath: filepath.Join(runtimeDir, "account.json"),
		pollStatus:  filepath.Join(runtimeDir, "poll_state.json"),
	}
}

func (s *AccountStore) LoadAccount() (Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var acc Account
	data, err := os.ReadFile(s.accountPath)
	if err != nil {
		if os.IsNotExist(err) {
			return acc, nil
		}
		return acc, err
	}
	err = json.Unmarshal(data, &acc)
	return acc, err
}

func (s *AccountStore) SaveAccount(acc Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc.SavedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.accountPath, data, 0644)
}

func (s *AccountStore) LoadGetUpdatesBuf() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var state PollState
	data, err := os.ReadFile(s.pollStatus)
	if err != nil {
		return ""
	}
	json.Unmarshal(data, &state)
	return state.GetUpdatesBuf
}

func (s *AccountStore) SaveGetUpdatesBuf(buf string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := PollState{
		GetUpdatesBuf: buf,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.pollStatus, data, 0644)
}

func (s *AccountStore) ClearGetUpdatesBuf() {
	s.mu.Lock()
	defer s.mu.Unlock()
	os.Remove(s.pollStatus)
}
