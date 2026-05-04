package wechat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestTestConnectionTreatsExpectedProbeTimeoutAsSuccess(t *testing.T) {
	service := newTestService(t, dummyRunner{})
	if err := service.configStore.Save(Config{
		LoginMode:   "manual",
		AccountID:   "wx-account",
		BotToken:    "token",
		BaseURL:     defaultBaseURL,
		WorkspaceID: "default",
		AgentID:     "claude",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	}))

	if err := service.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection() error = %v, want nil", err)
	}
}

func TestTestConnectionStillReturnsRealErrors(t *testing.T) {
	service := newTestService(t, dummyRunner{})
	if err := service.configStore.Save(Config{
		LoginMode:   "manual",
		AccountID:   "wx-account",
		BotToken:    "token",
		BaseURL:     defaultBaseURL,
		WorkspaceID: "default",
		AgentID:     "claude",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("tls handshake failed")
	}))

	err := service.TestConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tls handshake failed") {
		t.Fatalf("TestConnection() error = %v, want wrapped tls handshake failed", err)
	}
}
