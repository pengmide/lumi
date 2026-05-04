package wecom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/pengmide/lumi/internal/storage"
)

type ConversationStore struct {
	baseDir string
	mu      sync.Mutex
}

func NewConversationStore() *ConversationStore {
	return &ConversationStore{
		baseDir: filepath.Join(wecomRootDir(), "sessions"),
	}
}

func (s *ConversationStore) filePath(id string) string {
	return filepath.Join(s.baseDir, id+".json")
}

func (s *ConversationStore) Load(id string) (*storage.StoredSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath(id))
	if err != nil {
		return nil, err
	}
	var session storage.StoredSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *ConversationStore) Save(session *storage.StoredSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ensurePrivateDir(s.baseDir); err != nil {
		return err
	}
	return writePrivateJSON(s.filePath(session.ID), session)
}
