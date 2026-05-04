package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pengmide/lumi/internal/config"
	"github.com/pengmide/lumi/internal/conversation"
	"github.com/pengmide/lumi/internal/storage"
	workspacepreview "github.com/pengmide/lumi/internal/workspace"
)

func newShareTestServer(t *testing.T) *Server {
	t.Helper()

	baseDir := t.TempDir()
	workspaceDir := filepath.Join(baseDir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspaceDir, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "allowed.txt"), []byte("allowed content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "hidden.txt"), []byte("hidden content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "docs", "guide.md"), []byte("# Guide"), 0644); err != nil {
		t.Fatal(err)
	}

	server := &Server{
		config: &config.Config{
			Workspaces: []config.WorkspaceConfig{
				{ID: "ws", Name: "Workspace", Path: workspaceDir},
			},
			DefaultWorkspace: "ws",
		},
		conversations: conversation.NewManager(),
		sessionStore:  storage.NewSessionStore(filepath.Join(baseDir, "sessions")),
		shareStore:    storage.NewShareStore(filepath.Join(baseDir, "shares.json")),
		workspaceSvc:  workspacepreview.NewService(),
	}

	session := &storage.StoredSession{
		ID:          "conv-1",
		Title:       "Share test",
		WorkspaceID: "ws",
		Messages: []conversation.Message{
			{
				Role:    "user",
				Content: "please inspect this",
				Files: []conversation.MessageFile{
					{Name: "allowed.txt", Path: "allowed.txt", Size: int64(len("allowed content"))},
					{Name: "hidden.txt", Path: "hidden.txt", Size: int64(len("hidden content"))},
				},
			},
			{Role: "assistant", Content: "done"},
		},
		ActiveAgent: "claude",
		CreatedAt:   1,
		UpdatedAt:   2,
	}
	if err := server.sessionStore.Save(session); err != nil {
		t.Fatal(err)
	}

	return server
}

func createShare(t *testing.T, server *Server, files []storage.StoredSharedFile) conversationShareResponse {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"conversationId": "conv-1",
		"files":          files,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/shares/conversations", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleConversationShares(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create share status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Share conversationShareResponse `json:"share"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Share
}

func getPublicShare(t *testing.T, server *Server, token string) publicSharedConversationResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/public/shares/conversations/"+token, nil)
	rec := httptest.NewRecorder()
	server.handlePublicConversationShares(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public share status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response publicSharedConversationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestConversationShareStoresExplicitFiles(t *testing.T) {
	t.Parallel()

	server := newShareTestServer(t)

	share := createShare(t, server, []storage.StoredSharedFile{
		{Path: "allowed.txt"},
		{Path: "docs/../allowed.txt"},
		{Path: "docs/guide.md"},
	})
	if len(share.Files) != 2 {
		t.Fatalf("share files length = %d, want 2", len(share.Files))
	}
	if share.Files[0].Path != "allowed.txt" || share.Files[1].Path != "docs/guide.md" {
		t.Fatalf("share files = %#v", share.Files)
	}

	public := getPublicShare(t, server, share.Token)
	if len(public.Files) != 2 {
		t.Fatalf("public files length = %d, want 2", len(public.Files))
	}
	if len(public.Messages) != 2 {
		t.Fatalf("public messages length = %d, want 2", len(public.Messages))
	}
	if got := len(public.Messages[0].Files); got != 1 {
		t.Fatalf("public first message files length = %d, want 1", got)
	}
	if public.Messages[0].Files[0].Name != "allowed.txt" {
		t.Fatalf("public message file = %#v", public.Messages[0].Files[0])
	}
}

func TestConversationShareAllowsNoFiles(t *testing.T) {
	t.Parallel()

	server := newShareTestServer(t)

	share := createShare(t, server, nil)
	public := getPublicShare(t, server, share.Token)
	if len(public.Files) != 0 {
		t.Fatalf("public files length = %d, want 0", len(public.Files))
	}
	if got := len(public.Messages[0].Files); got != 0 {
		t.Fatalf("public first message files length = %d, want 0", got)
	}
}

func TestConversationShareUpdateReusesToken(t *testing.T) {
	t.Parallel()

	server := newShareTestServer(t)

	first := createShare(t, server, []storage.StoredSharedFile{{Path: "allowed.txt"}})
	second := createShare(t, server, []storage.StoredSharedFile{{Path: "docs/guide.md"}})
	if second.Token != first.Token {
		t.Fatalf("updated share token = %q, want %q", second.Token, first.Token)
	}
	if len(second.Files) != 1 || second.Files[0].Path != "docs/guide.md" {
		t.Fatalf("updated files = %#v", second.Files)
	}

	public := getPublicShare(t, server, second.Token)
	if len(public.Files) != 1 || public.Files[0].Name != "guide.md" {
		t.Fatalf("public files = %#v", public.Files)
	}
}

func TestConversationShareRejectsInvalidFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "directory", path: "docs"},
		{name: "missing", path: "missing.txt"},
		{name: "path escape", path: "../outside.txt"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := newShareTestServer(t)
			body, err := json.Marshal(map[string]any{
				"conversationId": "conv-1",
				"files":          []storage.StoredSharedFile{{Path: tt.path}},
			})
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/shares/conversations", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			server.handleConversationShares(rec, req)
			if rec.Code < 400 {
				t.Fatalf("status = %d, want error; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPublicSharedFilePreviewRequiresAllowlist(t *testing.T) {
	t.Parallel()

	server := newShareTestServer(t)
	share := createShare(t, server, []storage.StoredSharedFile{{Path: "allowed.txt"}})

	hiddenID := buildSharedConversationFileID(conversation.MessageFile{
		Name: "hidden.txt",
		Path: "hidden.txt",
		Size: int64(len("hidden content")),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/public/shares/conversations/"+share.Token+"/file-meta?fileId="+hiddenID, nil)
	rec := httptest.NewRecorder()
	server.handlePublicConversationShares(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}
