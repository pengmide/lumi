package sandbox

import (
	"context"
	"log"
	"time"
)

func (m *Manager) runScheduler() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer close(m.done)

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.gcExpired()
		}
	}
}

func (m *Manager) gcExpired() {
	now := time.Now().UnixMilli()
	expired := make([]string, 0)

	m.mu.Lock()
	for workspaceID, record := range m.runtimes {
		if record.ExpiresAt > 0 && record.ExpiresAt <= now && record.Status == StatusRunning {
			expired = append(expired, workspaceID)
		}
	}
	m.mu.Unlock()

	for _, workspaceID := range expired {
		if err := m.Terminate(context.Background(), workspaceID); err != nil {
			log.Printf("sandbox GC failed for %s: %v", workspaceID, err)
		}
	}
}
