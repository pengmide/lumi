package wechat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const maxProcessedMessageIDs = 500

type RuntimeState struct {
	Running             bool     `json:"running"`
	LastError           string   `json:"lastError,omitempty"`
	LastSyncAt          int64    `json:"lastSyncAt,omitempty"`
	LastLoginAt         int64    `json:"lastLoginAt,omitempty"`
	LastMessageAt       int64    `json:"lastMessageAt,omitempty"`
	Buf                 string   `json:"buf,omitempty"`
	ProcessedMessageIDs []string `json:"processedMessageIds,omitempty"`
}

type RuntimeStore struct {
	path string
	mu   sync.Mutex
}

func NewRuntimeStore() *RuntimeStore {
	return &RuntimeStore{
		path: filepath.Join(wechatRootDir(), "runtime.json"),
	}
}

func DefaultRuntimeState() RuntimeState {
	return RuntimeState{}
}

func (s *RuntimeStore) Load() (RuntimeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := DefaultRuntimeState()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return DefaultRuntimeState(), err
	}
	state.ProcessedMessageIDs = trimProcessedMessageIDs(state.ProcessedMessageIDs)
	return state, nil
}

func (s *RuntimeStore) Save(state RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state.ProcessedMessageIDs = trimProcessedMessageIDs(state.ProcessedMessageIDs)
	return writePrivateJSON(s.path, state)
}

func trimProcessedMessageIDs(ids []string) []string {
	if len(ids) <= maxProcessedMessageIDs {
		return ids
	}
	return append([]string(nil), ids[len(ids)-maxProcessedMessageIDs:]...)
}
