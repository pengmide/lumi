package sandbox

import "testing"

func TestActiveRuntimeWorkspaceIDsExcludesTerminated(t *testing.T) {
	manager := &Manager{
		runtimes: map[string]*RuntimeRecord{
			"running":     {WorkspaceID: "running", Status: StatusRunning},
			"pending":     {WorkspaceID: "pending", Status: StatusPending},
			"failed":      {WorkspaceID: "failed", Status: StatusFailed},
			"terminated":  {WorkspaceID: "terminated", Status: StatusTerminated},
			"terminating": {WorkspaceID: "terminating", Status: StatusTerminating},
		},
	}

	got := manager.activeRuntimeWorkspaceIDs()
	want := []string{"failed", "pending", "running", "terminating"}
	if len(got) != len(want) {
		t.Fatalf("activeRuntimeWorkspaceIDs() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("activeRuntimeWorkspaceIDs() = %#v, want %#v", got, want)
		}
	}
}

func TestShouldRemoveRecoveredContainer(t *testing.T) {
	now := int64(1000)

	tests := []struct {
		name   string
		record RuntimeRecord
		want   bool
	}{
		{
			name:   "terminated records should not keep containers",
			record: RuntimeRecord{Status: StatusTerminated},
			want:   true,
		},
		{
			name:   "expired running records are collected on startup",
			record: RuntimeRecord{Status: StatusRunning, ExpiresAt: now},
			want:   true,
		},
		{
			name:   "active running records are kept",
			record: RuntimeRecord{Status: StatusRunning, ExpiresAt: now + 1},
			want:   false,
		},
		{
			name:   "running records without expiry are kept",
			record: RuntimeRecord{Status: StatusRunning},
			want:   false,
		},
		{
			name:   "pending records are recovered for next ensure",
			record: RuntimeRecord{Status: StatusPending, ExpiresAt: now - 1},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRemoveRecoveredContainer(tt.record, now); got != tt.want {
				t.Fatalf("shouldRemoveRecoveredContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}
