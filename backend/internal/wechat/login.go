package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type loginManager struct {
	service *Service
	mu      sync.Mutex
	current *loginTask
}

type loginTask struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
}

func newLoginManager(service *Service) *loginManager {
	return &loginManager{service: service}
}

func (m *loginManager) Start() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		m.current.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &loginTask{
		id:     "wxlogin_" + strings.ReplaceAll(randomUUID(), "-", ""),
		ctx:    ctx,
		cancel: cancel,
	}
	m.current = task
	return task.id
}

func (m *loginManager) ServeEvents(w http.ResponseWriter, r *http.Request, id string) {
	send, ok := setupSSE(w)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	task := m.taskByID(id)
	if task == nil {
		send("error", map[string]string{"message": "login task not found"})
		send("done", map[string]any{})
		return
	}

	defer m.finish(id)
	ctx, cancel := context.WithCancel(task.ctx)
	defer cancel()
	go func() {
		<-r.Context().Done()
		task.cancel()
		cancel()
	}()

	cfg, err := m.service.configStore.Load()
	if err != nil {
		send("error", map[string]string{"message": err.Error()})
		send("done", map[string]any{})
		return
	}

	client := NewLoginClient(cfg.BaseURL)
	qr, err := client.GetQRCode(ctx)
	if err != nil {
		send("error", map[string]string{"message": err.Error()})
		send("done", map[string]any{})
		return
	}
	send("qr", map[string]string{"ticket": qr.Ticket, "imageUrl": qr.ImageURL})

	scanned := false
	for {
		status, err := client.GetQRCodeStatus(ctx, qr.Ticket)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			send("error", map[string]string{"message": err.Error()})
			send("done", map[string]any{})
			return
		}

		switch status.Status {
		case "wait", "":
		case "scaned":
			if !scanned {
				send("scanned", map[string]any{})
				scanned = true
			}
		case "expired":
			send("expired", map[string]any{})
			send("done", map[string]any{})
			return
		case "confirmed":
			cfg.AccountID = status.AccountID
			cfg.BotToken = status.BotToken
			if status.BaseURL != "" {
				cfg.BaseURL = status.BaseURL
			}
			if err := m.service.configStore.Save(cfg); err != nil {
				send("error", map[string]string{"message": err.Error()})
				send("done", map[string]any{})
				return
			}
			_ = m.service.updateRuntime(func(state *RuntimeState) {
				state.LastLoginAt = time.Now().UnixMilli()
			})
			send("confirmed", map[string]any{
				"accountId": cfg.AccountID,
				"baseUrl":   normalizeConfig(cfg).BaseURL,
				"hasToken":  strings.TrimSpace(cfg.BotToken) != "",
			})
			send("done", map[string]any{})
			return
		default:
			send("error", map[string]string{"message": fmt.Sprintf("unexpected qr status: %s", status.Status)})
			send("done", map[string]any{})
			return
		}
	}
}

func (m *loginManager) taskByID(id string) *loginTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.id != id {
		return nil
	}
	return m.current
}

func (m *loginManager) finish(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil && m.current.id == id {
		m.current.cancel()
		m.current = nil
	}
}

func setupSSE(w http.ResponseWriter) (func(string, any), bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}

	return func(event string, data any) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
		flusher.Flush()
	}, true
}
