package device

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pengmide/lumi/internal/setupcheck"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func TestWebSocketRegisterSetupHeartbeatAndDisconnect(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	server := httptest.NewServer(http.HandlerFunc(registry.HandleWebSocket))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-secret")

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	registerEnv, err := NewEnvelope(MsgDeviceRegister, "msg-register", "dev-1", "", DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	})
	if err != nil {
		t.Fatalf("NewEnvelope(register) error = %v", err)
	}
	if err := wsjson.Write(ctx, conn, registerEnv); err != nil {
		t.Fatalf("wsjson.Write(register) error = %v", err)
	}

	var ack Envelope
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("wsjson.Read(register ack) error = %v", err)
	}
	if ack.Type != MsgAck || ack.ID != "msg-register" {
		t.Fatalf("ack = %+v, want ack for register", ack)
	}

	device, ok := registry.GetDevice("dev-1")
	if !ok {
		t.Fatalf("GetDevice() ok = false, want true")
	}
	if device.Status != StatusSetupRequired {
		t.Fatalf("device.Status after register = %q, want %q", device.Status, StatusSetupRequired)
	}

	setupEnv, err := NewEnvelope(MsgSetupStatus, "msg-setup", "dev-1", "", setupcheck.SetupStatus{
		Ready: true,
	})
	if err != nil {
		t.Fatalf("NewEnvelope(setup) error = %v", err)
	}
	if err := wsjson.Write(ctx, conn, setupEnv); err != nil {
		t.Fatalf("wsjson.Write(setup) error = %v", err)
	}
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("wsjson.Read(setup ack) error = %v", err)
	}

	device, _ = registry.GetDevice("dev-1")
	if device.Status != StatusOnline {
		t.Fatalf("device.Status after setup = %q, want %q", device.Status, StatusOnline)
	}

	heartbeatEnv, err := NewEnvelope(MsgDeviceHeartbeat, "msg-heartbeat", "dev-1", "", DeviceHeartbeatPayload{
		RunningTaskIDs: []string{"task-1"},
	})
	if err != nil {
		t.Fatalf("NewEnvelope(heartbeat) error = %v", err)
	}
	if err := wsjson.Write(ctx, conn, heartbeatEnv); err != nil {
		t.Fatalf("wsjson.Write(heartbeat) error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = registry.GetDevice("dev-1")
		if device.Status == StatusBusy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if device.Status != StatusBusy {
		t.Fatalf("device.Status after heartbeat = %q, want %q", device.Status, StatusBusy)
	}

	if err := conn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("conn.Close() error = %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		device, _ = registry.GetDevice("dev-1")
		if device.Status == StatusOffline {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if device.Status != StatusOffline {
		t.Fatalf("device.Status after disconnect = %q, want %q", device.Status, StatusOffline)
	}
}

func TestSendSetupCheckToConnectedDevice(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	server := httptest.NewServer(http.HandlerFunc(registry.HandleWebSocket))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-secret")

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	registerEnv, _ := NewEnvelope(MsgDeviceRegister, "msg-register", "dev-1", "", DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	})
	if err := wsjson.Write(ctx, conn, registerEnv); err != nil {
		t.Fatalf("wsjson.Write(register) error = %v", err)
	}
	var ack Envelope
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("wsjson.Read(register ack) error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		var env Envelope
		if err := wsjson.Read(ctx, conn, &env); err != nil {
			done <- err
			return
		}
		if env.Type != MsgSetupCheck {
			done <- errUnexpectedType(env.Type, MsgSetupCheck)
			return
		}
		if err := wsjson.Write(ctx, conn, AckEnvelope(env.ID)); err != nil {
			done <- err
			return
		}
		setupEnv, err := NewEnvelope(MsgSetupStatus, "msg-setup", "dev-1", "", setupcheck.SetupStatus{Ready: true})
		if err != nil {
			done <- err
			return
		}
		if err := wsjson.Write(ctx, conn, setupEnv); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	if err := registry.SendToDevice(ctx, "dev-1", MsgSetupCheck, "", SetupCheckPayload{}); err != nil {
		t.Fatalf("SendToDevice(setup.check) error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("device goroutine error = %v", err)
	}

	var setupAck Envelope
	if err := wsjson.Read(ctx, conn, &setupAck); err != nil {
		t.Fatalf("wsjson.Read(setup ack) error = %v", err)
	}
	if setupAck.Type != MsgAck {
		t.Fatalf("setupAck.Type = %q, want %q", setupAck.Type, MsgAck)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		device, _ := registry.GetDevice("dev-1")
		if device.Status == StatusOnline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	device, _ := registry.GetDevice("dev-1")
	t.Fatalf("device.Status = %q, want %q", device.Status, StatusOnline)
}

func errUnexpectedType(got, want MessageType) error {
	return &unexpectedTypeError{got: got, want: want}
}

type unexpectedTypeError struct {
	got  MessageType
	want MessageType
}

func (e *unexpectedTypeError) Error() string {
	return "unexpected message type: got " + string(e.got) + ", want " + string(e.want)
}
