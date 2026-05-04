package wechat

import (
	"fmt"
	"testing"
)

func TestRuntimeStorePersistsBufAndTrimsProcessedIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store := NewRuntimeStore()
	state := DefaultRuntimeState()
	state.Buf = "next-buf"
	for i := 0; i < 520; i++ {
		state.ProcessedMessageIDs = append(state.ProcessedMessageIDs, fmt.Sprintf("msg-%03d", i))
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Buf != "next-buf" {
		t.Fatalf("Buf = %q", loaded.Buf)
	}
	if len(loaded.ProcessedMessageIDs) != 500 {
		t.Fatalf("len(ProcessedMessageIDs) = %d, want 500", len(loaded.ProcessedMessageIDs))
	}
	if loaded.ProcessedMessageIDs[0] != "msg-020" {
		t.Fatalf("first processed id = %q, want msg-020", loaded.ProcessedMessageIDs[0])
	}
}
