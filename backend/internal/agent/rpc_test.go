package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pengmide/lumi/internal/config"
	"github.com/pengmide/lumi/internal/jsonrpc"
)

type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

func TestHandlePermissionRequestSupportsImmediateConfirm(t *testing.T) {
	proc := NewProcess(&config.AgentConfig{
		ID:      "claude",
		Name:    "Claude",
		Command: "echo",
	})
	var out bytes.Buffer
	proc.stdin = &nopWriteCloser{Buffer: &out}

	done := make(chan struct{})
	cleanup := proc.OnPermission(func(req *PermissionRequest) {
		proc.ConfirmPermission(req.ToolCall.ToolCallID, "allow-once")
		close(done)
	})
	defer cleanup()

	msg := jsonrpc.NewRequest(1, "session/request_permission", map[string]any{
		"sessionId": "session-1",
		"options": []map[string]any{
			{
				"optionId": "allow-once",
				"name":     "Allow once",
				"kind":     "allow_once",
			},
		},
		"toolCall": map[string]any{
			"toolCallId": "tool-1",
			"title":      "Run command",
		},
	})
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var message jsonrpc.Message
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	go proc.handlePermissionRequest(&message)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("permission handler did not receive request")
	}

	deadline := time.After(2 * time.Second)
	for {
		if strings.Contains(out.String(), `"optionId":"allow-once"`) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("permission response was not written: %s", out.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	proc.mu.Lock()
	_, ok := proc.permissions["tool-1"]
	proc.mu.Unlock()
	if ok {
		t.Fatal("permission request should be removed after confirm")
	}
}
