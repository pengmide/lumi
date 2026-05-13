package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pengmide/lumi/internal/config"
)

func TestDialAndSubscribeTimesOutWaitingForSubscribeResponse(t *testing.T) {
	upgrader := websocket.Upgrader{}
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error = %v", err)
			return
		}
		defer close(closed)
		defer conn.Close()

		var frame wsFrame
		if err := conn.ReadJSON(&frame); err != nil {
			t.Errorf("read subscribe frame error = %v", err)
			return
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	originalEndpoint := wsEndpoint
	originalDialer := wsDialer
	wsEndpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	wsDialer = websocket.DefaultDialer
	t.Cleanup(func() {
		wsEndpoint = originalEndpoint
		wsDialer = originalDialer
	})

	rt := &wsRuntime{
		cfg: Config{
			BotID:            "bot-1",
			BotSecret:        "secret-1",
			ConnectTimeoutMs: 100,
		},
	}

	start := time.Now()
	conn, err := rt.dialAndSubscribe(context.Background())
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("dialAndSubscribe() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "subscribe response") {
		t.Fatalf("error = %q, want subscribe response timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("dialAndSubscribe() elapsed = %s, want under 2s", elapsed)
	}

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("server connection was not closed after subscribe timeout")
	}
}

func TestStopClosesIdleWebSocketConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	upgrader := websocket.Upgrader{}
	subscribed := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error = %v", err)
			return
		}
		defer close(closed)
		defer conn.Close()

		var frame wsFrame
		if err := conn.ReadJSON(&frame); err != nil {
			t.Errorf("read subscribe frame error = %v", err)
			return
		}
		resp := wsFrame{ErrCode: intPtr(0)}
		raw, _ := json.Marshal(resp)
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Errorf("write subscribe response error = %v", err)
			return
		}
		close(subscribed)
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	originalEndpoint := wsEndpoint
	originalDialer := wsDialer
	wsEndpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	wsDialer = websocket.DefaultDialer
	t.Cleanup(func() {
		wsEndpoint = originalEndpoint
		wsDialer = originalDialer
	})

	cfg := &config.Config{
		Workspaces: []config.WorkspaceConfig{
			{ID: "default", Name: "Default", Path: t.TempDir()},
		},
		Agents: []config.AgentConfig{
			{ID: "agent-1", Name: "Agent"},
		},
	}
	svc := NewService(cfg, &noopChatRunner{})
	if err := svc.configStore.Save(Config{
		Enabled:             true,
		Mode:                defaultMode,
		BotID:               "bot-1",
		BotSecret:           "secret-1",
		WorkspaceID:         "default",
		AgentID:             "agent-1",
		ConnectTimeoutMs:    1000,
		HeartbeatIntervalMs: 30000,
		MessageAckTimeoutMs: 1000,
	}); err != nil {
		t.Fatalf("save config error = %v", err)
	}

	if err := svc.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("service did not subscribe")
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return after closing idle websocket")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("server connection was not closed")
	}
}

type noopChatRunner struct{}

func (r *noopChatRunner) RunWeComChat(context.Context, ChatRunInput, ChatEventSink) error {
	return nil
}

func intPtr(v int) *int {
	return &v
}
