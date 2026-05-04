package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
