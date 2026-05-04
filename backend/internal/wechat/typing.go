package wechat

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

var (
	typingInterval             = 10 * time.Second
	typingRetryDelay           = 500 * time.Millisecond
	typingMaxRetries           = 2
	typingConfigCacheTTL       = 24 * time.Hour
	typingConfigInitialBackoff = 2 * time.Second
	typingConfigMaxBackoff     = 1 * time.Hour
	typingStopWait             = 2 * time.Second
)

const (
	typingStatusActive = 1
	typingStatusCancel = 2
)

type typingManager struct {
	mu     sync.Mutex
	cache  map[string]typingCacheEntry
	active map[string]*typingSession
}

type typingCacheEntry struct {
	ticket      string
	nextFetchAt time.Time
	retryDelay  time.Duration
}

type typingSession struct {
	token  string
	userID string
	client *Client
	ticket string
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func newTypingManager(_ *Service) *typingManager {
	return &typingManager{
		cache:  make(map[string]typingCacheEntry),
		active: make(map[string]*typingSession),
	}
}

func (m *typingManager) Start(parentCtx context.Context, cfg Config, userID, contextToken string) func() {
	if strings.TrimSpace(userID) == "" {
		return func() {}
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if err := parentCtx.Err(); err != nil {
		return func() {}
	}

	if existing := m.activeSession(userID); existing != nil {
		m.stopSession(existing)
	}

	client := NewClient(cfg)
	ticket := m.getTypingTicket(parentCtx, client, userID, contextToken)
	if ticket == "" {
		return func() {}
	}

	sessionCtx, cancel := context.WithCancel(parentCtx)
	session := &typingSession{
		token:  randomUUID(),
		userID: userID,
		client: client,
		ticket: ticket,
		ctx:    sessionCtx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	m.mu.Lock()
	m.active[userID] = session
	m.mu.Unlock()

	go m.runSession(session)

	return func() {
		m.stopSession(session)
	}
}

func (m *typingManager) StopAll() {
	m.mu.Lock()
	sessions := make([]*typingSession, 0, len(m.active))
	for _, session := range m.active {
		sessions = append(sessions, session)
	}
	m.active = make(map[string]*typingSession)
	m.mu.Unlock()

	for _, session := range sessions {
		session.cancel()
		select {
		case <-session.done:
		case <-time.After(typingStopWait):
		}
	}
}

func (m *typingManager) activeSession(userID string) *typingSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[userID]
}

func (m *typingManager) stopSession(session *typingSession) {
	if session == nil {
		return
	}

	m.mu.Lock()
	if current, ok := m.active[session.userID]; ok && current.token == session.token {
		delete(m.active, session.userID)
	}
	m.mu.Unlock()

	session.cancel()

	select {
	case <-session.done:
	case <-time.After(typingStopWait):
	}
}

func (m *typingManager) runSession(session *typingSession) {
	defer close(session.done)
	defer m.clearActiveSession(session)

	m.sendTypingRetry(session.ctx, session.client, session.userID, session.ticket)

	ticker := time.NewTicker(typingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-session.ctx.Done():
			m.sendCancel(session.client, session.userID, session.ticket)
			return
		case <-ticker.C:
			m.sendTypingRetry(session.ctx, session.client, session.userID, session.ticket)
		}
	}
}

func (m *typingManager) clearActiveSession(session *typingSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.active[session.userID]; ok && current.token == session.token {
		delete(m.active, session.userID)
	}
}

func (m *typingManager) getTypingTicket(ctx context.Context, client *Client, userID, contextToken string) string {
	now := time.Now()

	m.mu.Lock()
	entry, ok := m.cache[userID]
	if ok && now.Before(entry.nextFetchAt) {
		ticket := entry.ticket
		m.mu.Unlock()
		return ticket
	}
	m.mu.Unlock()

	ticket, err := client.GetTypingTicket(ctx, userID, contextToken)
	if err == nil {
		m.mu.Lock()
		m.cache[userID] = typingCacheEntry{
			ticket:      ticket,
			nextFetchAt: now.Add(typingConfigCacheTTL),
			retryDelay:  typingConfigInitialBackoff,
		}
		m.mu.Unlock()
		return ticket
	}

	log.Printf("wechat: get typing ticket failed for %s: %v", userID, err)

	m.mu.Lock()
	defer m.mu.Unlock()
	entry = m.cache[userID]
	nextDelay := typingConfigInitialBackoff
	if entry.retryDelay > 0 {
		nextDelay = entry.retryDelay * 2
		if nextDelay > typingConfigMaxBackoff {
			nextDelay = typingConfigMaxBackoff
		}
	}
	m.cache[userID] = typingCacheEntry{
		ticket:      entry.ticket,
		nextFetchAt: now.Add(nextDelay),
		retryDelay:  nextDelay,
	}
	return entry.ticket
}

func (m *typingManager) sendTypingRetry(ctx context.Context, client *Client, userID, ticket string) {
	if ticket == "" {
		return
	}

	delay := typingRetryDelay
	for attempt := 0; attempt <= typingMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if err := client.SendTyping(ctx, userID, ticket, typingStatusActive); err == nil {
			return
		} else if attempt == typingMaxRetries {
			log.Printf("wechat: send typing active failed for %s: %v", userID, err)
			return
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay *= 2
	}
}

func (m *typingManager) sendCancel(client *Client, userID, ticket string) {
	if ticket == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	if err := client.SendTyping(ctx, userID, ticket, typingStatusCancel); err != nil {
		log.Printf("wechat: send typing cancel failed for %s: %v", userID, err)
	}
}
