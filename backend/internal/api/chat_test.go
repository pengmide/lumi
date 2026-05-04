package api

import (
	"context"
	"testing"

	"github.com/pengmide/lumi/internal/agentmode"
)

func TestShouldAutoApproveAgent(t *testing.T) {
	server := newTestAPIServer(t)
	server.config.FindAgent("claude").SessionMode = agentmode.ClaudeModeBypassPermissions
	server.config.FindAgent("codex").SessionMode = agentmode.CodexModeYolo

	if !server.shouldAutoApproveAgent("claude") {
		t.Fatalf("shouldAutoApproveAgent(claude) = false, want true")
	}
	if !server.shouldAutoApproveAgent("codex") {
		t.Fatalf("shouldAutoApproveAgent(codex) = false, want true")
	}
	if server.shouldAutoApproveAgent("missing") {
		t.Fatalf("shouldAutoApproveAgent(missing) = true, want false")
	}
}

func TestCollectFileMentionsSkipsAgentsAndDeduplicates(t *testing.T) {
	server := newTestAPIServer(t)

	got := collectFileMentions(
		"Review @claude and @src/app.ts plus @README.md and @src/app.ts again",
		server.router,
	)

	want := []string{"src/app.ts", "README.md"}
	if len(got) != len(want) {
		t.Fatalf("len(mentions) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mentions[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestPrepareChatLeavesLocalWorkspaceMentionsUnchanged(t *testing.T) {
	server := newTestAPIServer(t)

	prepared, err := server.prepareChat(context.Background(), chatRequest{
		Message:     "Review @README.md",
		WorkspaceID: "default",
	})
	if err != nil {
		t.Fatalf("prepareChat() error = %v", err)
	}
	if prepared.PromptText != "Review @README.md" {
		t.Fatalf("prepared.PromptText = %q, want original message", prepared.PromptText)
	}
}
