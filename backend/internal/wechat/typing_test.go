package wechat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestTypingManagerCachesTicketAndCancelsOnStop(t *testing.T) {
	restoreTypingTestConfig(t, 20*time.Millisecond, 5*time.Millisecond, 24*time.Hour, 20*time.Millisecond)

	var mu sync.Mutex
	getConfigCalls := 0
	statuses := make([]int, 0, 6)

	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		switch req.URL.Path {
		case "/ilink/bot/getconfig":
			mu.Lock()
			getConfigCalls++
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0,"typing_ticket":"ticket-1"}`), nil
		case "/ilink/bot/sendtyping":
			var body map[string]any
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				t.Fatalf("Unmarshal(sendtyping body) error = %v", err)
			}
			mu.Lock()
			statuses = append(statuses, int(body["status"].(float64)))
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0}`), nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.String())
			return nil, nil
		}
	}))

	manager := newTypingManager(nil)
	cfg := Config{AccountID: "wx-bot", BotToken: "bot-token", BaseURL: "https://wechat.test"}

	stop := manager.Start(context.Background(), cfg, "user-1", "ctx-1")
	waitUntil(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 2
	})
	stop()
	waitUntil(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 3 && statuses[len(statuses)-1] == typingStatusCancel
	})

	stop = manager.Start(context.Background(), cfg, "user-1", "ctx-2")
	waitUntil(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 4
	})
	stop()
	waitUntil(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 5 && statuses[len(statuses)-1] == typingStatusCancel
	})

	mu.Lock()
	defer mu.Unlock()
	if getConfigCalls != 1 {
		t.Fatalf("getConfigCalls = %d, want 1", getConfigCalls)
	}
}

func TestTypingManagerRetriesActiveSend(t *testing.T) {
	restoreTypingTestConfig(t, time.Hour, 5*time.Millisecond, 24*time.Hour, 20*time.Millisecond)

	var mu sync.Mutex
	activeAttempts := 0

	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/ilink/bot/getconfig":
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0,"typing_ticket":"ticket-retry"}`), nil
		case "/ilink/bot/sendtyping":
			var body map[string]any
			bodyBytes, _ := io.ReadAll(req.Body)
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				t.Fatalf("Unmarshal(sendtyping body) error = %v", err)
			}
			if int(body["status"].(float64)) == typingStatusActive {
				mu.Lock()
				activeAttempts++
				current := activeAttempts
				mu.Unlock()
				if current < 3 {
					return nil, context.DeadlineExceeded
				}
			}
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0}`), nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.String())
			return nil, nil
		}
	}))

	manager := newTypingManager(nil)
	cfg := Config{AccountID: "wx-bot", BotToken: "bot-token", BaseURL: "https://wechat.test"}
	stop := manager.Start(context.Background(), cfg, "user-retry", "ctx-retry")
	waitUntil(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return activeAttempts == 3
	})
	stop()
}

func TestTypingManagerStopAllCancelsActiveSessions(t *testing.T) {
	restoreTypingTestConfig(t, time.Hour, 5*time.Millisecond, 24*time.Hour, 20*time.Millisecond)

	var mu sync.Mutex
	cancelCount := 0
	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/ilink/bot/getconfig":
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0,"typing_ticket":"ticket-stop-all"}`), nil
		case "/ilink/bot/sendtyping":
			bodyBytes, _ := io.ReadAll(req.Body)
			var body map[string]any
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				t.Fatalf("Unmarshal(sendtyping body) error = %v", err)
			}
			if int(body["status"].(float64)) == typingStatusCancel {
				mu.Lock()
				cancelCount++
				mu.Unlock()
			}
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0}`), nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.String())
			return nil, nil
		}
	}))

	manager := newTypingManager(nil)
	cfg := Config{AccountID: "wx-bot", BotToken: "bot-token", BaseURL: "https://wechat.test"}
	stop := manager.Start(context.Background(), cfg, "user-stop", "ctx-stop")
	defer stop()

	waitUntil(t, 500*time.Millisecond, func() bool {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return len(manager.active) == 1
	})
	manager.StopAll()
	waitUntil(t, 500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return cancelCount == 1
	})
}

func restoreTypingTestConfig(t *testing.T, interval, retryDelay, cacheTTL, stopWait time.Duration) {
	t.Helper()

	originalInterval := typingInterval
	originalRetryDelay := typingRetryDelay
	originalCacheTTL := typingConfigCacheTTL
	originalStopWait := typingStopWait
	originalMaxRetries := typingMaxRetries

	typingInterval = interval
	typingRetryDelay = retryDelay
	typingConfigCacheTTL = cacheTTL
	typingStopWait = stopWait
	typingMaxRetries = 2

	t.Cleanup(func() {
		typingInterval = originalInterval
		typingRetryDelay = originalRetryDelay
		typingConfigCacheTTL = originalCacheTTL
		typingStopWait = originalStopWait
		typingMaxRetries = originalMaxRetries
	})
}
