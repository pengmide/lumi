package device

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/pengmide/lumi/internal/setupcheck"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()

	store := NewStore(filepath.Join(t.TempDir(), "devices.json"))
	registry, err := NewRegistry(store, "test-secret")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func newTestConnection() *Connection {
	return NewConnection(nil)
}

func TestNewRegistryMarksPersistedDevicesOffline(t *testing.T) {
	t.Parallel()

	store := NewStore(filepath.Join(t.TempDir(), "devices.json"))
	err := store.Save([]Device{{
		ID:         "dev-1",
		Name:       "Office Mac",
		Status:     StatusOnline,
		SetupReady: true,
	}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	registry, err := NewRegistry(store, "secret")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	device, ok := registry.GetDevice("dev-1")
	if !ok {
		t.Fatalf("GetDevice() ok = false, want true")
	}
	if device.Status != StatusOffline {
		t.Fatalf("device.Status = %q, want %q", device.Status, StatusOffline)
	}
}

func TestRegisterDeviceAndSetupLifecycle(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	conn := newTestConnection()

	device, err := registry.RegisterDevice(conn, DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents: []DeviceAgentInfo{
			{ID: "claude", Name: "Claude Code"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}
	if device.Status != StatusSetupRequired {
		t.Fatalf("device.Status = %q, want %q", device.Status, StatusSetupRequired)
	}
	if device.SetupReady {
		t.Fatalf("device.SetupReady = true, want false")
	}

	err = registry.UpdateSetupStatus("dev-1", setupcheck.SetupStatus{Ready: false})
	if err != nil {
		t.Fatalf("UpdateSetupStatus(false) error = %v", err)
	}
	device, _ = registry.GetDevice("dev-1")
	if device.Status != StatusSetupRequired {
		t.Fatalf("device.Status after not-ready = %q, want %q", device.Status, StatusSetupRequired)
	}

	err = registry.UpdateSetupStatus("dev-1", setupcheck.SetupStatus{
		Ready:       true,
		Environment: []setupcheck.DependencyItem{{Name: "npm", Status: "ready"}},
	})
	if err != nil {
		t.Fatalf("UpdateSetupStatus(true) error = %v", err)
	}
	device, _ = registry.GetDevice("dev-1")
	if !device.SetupReady {
		t.Fatalf("device.SetupReady = false, want true")
	}
	if device.Status != StatusOnline {
		t.Fatalf("device.Status = %q, want %q", device.Status, StatusOnline)
	}
}

func TestTaskLifecycleAndMappings(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	_, err := registry.RegisterDevice(newTestConnection(), DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	})
	if err != nil {
		t.Fatalf("RegisterDevice() error = %v", err)
	}
	if err := registry.UpdateSetupStatus("dev-1", setupcheck.SetupStatus{Ready: true}); err != nil {
		t.Fatalf("UpdateSetupStatus() error = %v", err)
	}

	task1 := NewTaskRun("task-1", "dev-1", "conv-1", "claude", "ws-1", "/tmp/project")
	if err := registry.StartTask(task1); err != nil {
		t.Fatalf("StartTask(task1) error = %v", err)
	}

	task2 := NewTaskRun("task-2", "dev-1", "conv-1", "claude", "ws-1", "/tmp/project")
	if err := registry.StartTask(task2); !errors.Is(err, ErrDeviceBusy) {
		t.Fatalf("StartTask(task2) error = %v, want %v", err, ErrDeviceBusy)
	}

	registry.setTaskSession(task1.ID, "session-1")
	registry.RegisterPermission("tool-1", task1.ID)

	if task, ok := registry.TaskBySession("session-1"); !ok || task.ID != task1.ID {
		t.Fatalf("TaskBySession() = (%v, %v), want task-1", task, ok)
	}
	if task, ok := registry.TaskByToolCall("tool-1"); !ok || task.ID != task1.ID {
		t.Fatalf("TaskByToolCall() = (%v, %v), want task-1", task, ok)
	}

	registry.FinishTask(task1.ID)
	if _, ok := registry.TaskBySession("session-1"); ok {
		t.Fatalf("TaskBySession() ok = true after FinishTask, want false")
	}
	if _, ok := registry.TaskByToolCall("tool-1"); ok {
		t.Fatalf("TaskByToolCall() ok = true after FinishTask, want false")
	}

	device, _ := registry.GetDevice("dev-1")
	if device.Status != StatusOnline {
		t.Fatalf("device.Status after FinishTask = %q, want %q", device.Status, StatusOnline)
	}
}

func TestReconnectFailsRunningTask(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	conn1 := newTestConnection()
	_, err := registry.RegisterDevice(conn1, DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	})
	if err != nil {
		t.Fatalf("RegisterDevice(conn1) error = %v", err)
	}
	if err := registry.UpdateSetupStatus("dev-1", setupcheck.SetupStatus{Ready: true}); err != nil {
		t.Fatalf("UpdateSetupStatus() error = %v", err)
	}

	task := NewTaskRun("task-1", "dev-1", "conv-1", "claude", "ws-1", "/tmp/project")
	if err := registry.StartTask(task); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	_, err = registry.RegisterDevice(newTestConnection(), DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	})
	if err != nil {
		t.Fatalf("RegisterDevice(conn2) error = %v", err)
	}

	event := <-task.Events
	if event.Type != DeviceEventError {
		t.Fatalf("event.Type = %q, want %q", event.Type, DeviceEventError)
	}
	if event.Err == nil || event.Err.Error() != "Device reconnected" {
		t.Fatalf("event.Err = %v, want Device reconnected", event.Err)
	}
}

func TestMarkDisconnectedConnectionIgnoresStaleConnection(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	conn1 := newTestConnection()
	if _, err := registry.RegisterDevice(conn1, DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	}); err != nil {
		t.Fatalf("RegisterDevice(conn1) error = %v", err)
	}
	if err := registry.UpdateSetupStatus("dev-1", setupcheck.SetupStatus{Ready: true}); err != nil {
		t.Fatalf("UpdateSetupStatus(conn1) error = %v", err)
	}

	conn2 := newTestConnection()
	if _, err := registry.RegisterDevice(conn2, DeviceRegisterPayload{
		DeviceID: "dev-1",
		Name:     "Office Mac",
		Agents:   []DeviceAgentInfo{{ID: "claude", Name: "Claude Code"}},
	}); err != nil {
		t.Fatalf("RegisterDevice(conn2) error = %v", err)
	}
	if err := registry.UpdateSetupStatus("dev-1", setupcheck.SetupStatus{Ready: true}); err != nil {
		t.Fatalf("UpdateSetupStatus(conn2) error = %v", err)
	}

	registry.MarkDisconnectedConnection("dev-1", conn1, "connection closed")

	device, ok := registry.GetDevice("dev-1")
	if !ok {
		t.Fatalf("GetDevice() ok = false, want true")
	}
	if device.Status != StatusOnline {
		t.Fatalf("device.Status after stale disconnect = %q, want %q", device.Status, StatusOnline)
	}

	task := NewTaskRun("task-1", "dev-1", "conv-1", "claude", "ws-1", "/tmp/project")
	if err := registry.StartTask(task); err != nil {
		t.Fatalf("StartTask() after stale disconnect error = %v", err)
	}
}
