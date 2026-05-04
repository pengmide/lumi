package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pengmide/lumi/internal/conversation"
	"github.com/pengmide/lumi/internal/device"
	"github.com/pengmide/lumi/internal/sandbox"
	"github.com/pengmide/lumi/internal/storage"
	workspacepreview "github.com/pengmide/lumi/internal/workspace"
)

const publicShareHTMLRootSentinel = "__root__"

type conversationShareResponse struct {
	ID             string                     `json:"id"`
	Token          string                     `json:"token"`
	ConversationID string                     `json:"conversationId"`
	Files          []storage.StoredSharedFile `json:"files"`
	CreatedAt      int64                      `json:"createdAt"`
	UpdatedAt      int64                      `json:"updatedAt"`
}

type publicSharedConversationResponse struct {
	ID        string                     `json:"id"`
	Title     string                     `json:"title"`
	Files     []conversation.MessageFile `json:"files"`
	Messages  []conversation.Message     `json:"messages"`
	CreatedAt int64                      `json:"createdAt"`
	UpdatedAt int64                      `json:"updatedAt"`
}

type sharedConversationFile struct {
	ID           string
	Name         string
	OriginalPath string
	Size         int64
}

func (s *Server) handleConversationShares(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		ConversationID string                     `json:"conversationId"`
		Files          []storage.StoredSharedFile `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	session, err := s.loadSessionSnapshot(data.ConversationID)
	if err != nil {
		writeError(w, "Session not found", http.StatusNotFound)
		return
	}
	if !isSessionShareable(session) {
		writeError(w, "Conversation is not ready to share", http.StatusBadRequest)
		return
	}

	workspaceID := session.WorkspaceID
	runtimeInfo, err := s.resolveWorkspaceRuntime(r.Context(), workspaceID, r)
	if err != nil {
		writeResolvedRuntimeError(w, err)
		return
	}
	shareFiles, err := s.normalizeConversationShareFilesForRuntime(r.Context(), runtimeInfo, data.Files)
	if err != nil {
		writeRuntimeWorkspaceError(w, runtimeInfo, err)
		return
	}

	now := time.Now().UnixMilli()
	existing := s.shareStore.GetActiveByConversation(data.ConversationID)
	if existing != nil {
		existing.Files = shareFiles
		existing.WorkspaceID = workspaceID
		existing.UpdatedAt = now
		if err := s.shareStore.Put(*existing); err != nil {
			writeError(w, "Failed to update share", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"share": toConversationShareResponse(existing)})
		return
	}

	share := storage.StoredShare{
		ID:             generateUUID(),
		Token:          generateShareToken(),
		ConversationID: session.ID,
		WorkspaceID:    workspaceID,
		Files:          shareFiles,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.shareStore.Put(share); err != nil {
		writeError(w, "Failed to create share", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"share": toConversationShareResponse(&share)})
}

func (s *Server) handleConversationShareByConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimPrefix(r.URL.Path, "/api/shares/conversations/by-conversation/")
	if strings.TrimSpace(conversationID) == "" {
		writeError(w, "Conversation ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		session, err := s.loadSessionSnapshot(conversationID)
		if err != nil {
			writeError(w, "Session not found", http.StatusNotFound)
			return
		}
		if !isSessionShareable(session) {
			writeJSON(w, map[string]any{"share": nil})
			return
		}

		share := s.shareStore.GetActiveByConversation(conversationID)
		if share == nil {
			writeJSON(w, map[string]any{"share": nil})
			return
		}
		writeJSON(w, map[string]any{"share": toConversationShareResponse(share)})
	case "DELETE":
		revoked, err := s.shareStore.RevokeByConversation(conversationID, time.Now().UnixMilli())
		if err != nil {
			writeError(w, "Failed to revoke share", http.StatusInternalServerError)
			return
		}
		if revoked == nil {
			writeJSON(w, map[string]any{"success": true})
			return
		}
		writeJSON(w, map[string]any{"success": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePublicConversationShares(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/public/shares/conversations/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		writeError(w, "Share token required", http.StatusBadRequest)
		return
	}

	segments := strings.Split(trimmed, "/")
	token := segments[0]
	if token == "" {
		writeError(w, "Share token required", http.StatusBadRequest)
		return
	}

	if len(segments) == 1 {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublicSharedConversation(w, r, token)
		return
	}

	switch segments[1] {
	case "file-meta":
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublicSharedFileMeta(w, r, token)
	case "file-content":
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublicSharedFileContent(w, r, token)
	case "file-buffer":
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublicSharedFileBuffer(w, r, token)
	case "html-preview":
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublicSharedHTMLPreview(w, r, token)
	case "html-asset":
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublicSharedHTMLAsset(w, r, token, segments[2:])
	default:
		writeError(w, "Unsupported shared resource", http.StatusNotFound)
	}
}

func (s *Server) handlePublicSharedConversation(w http.ResponseWriter, r *http.Request, token string) {
	share, session, files, _, err := s.loadActiveSharedConversationRuntime(r.Context(), r, token)
	if err != nil {
		if writePublicShareRuntimeError(w, err) {
			return
		}
		writeError(w, "Shared conversation not found", http.StatusNotFound)
		return
	}

	writeJSON(w, publicSharedConversationResponse{
		ID:        share.ConversationID,
		Title:     session.Title,
		Files:     buildPublicSharedFileList(files),
		Messages:  buildPublicSharedMessages(session, files),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	})
}

func (s *Server) handlePublicSharedFileMeta(w http.ResponseWriter, r *http.Request, token string) {
	_, _, file, runtimeInfo, err := s.resolveSharedConversationFileRuntime(r.Context(), r, token, r.URL.Query().Get("fileId"))
	if err != nil {
		if writePublicShareRuntimeError(w, err) {
			return
		}
		writeError(w, "Shared file not found", http.StatusNotFound)
		return
	}

	meta, err := s.statRuntimeSharedFile(r.Context(), runtimeInfo, file.OriginalPath, file.ID)
	if err != nil {
		if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
			return
		}
		if runtimeInfo.Mode == "local" {
			writeWorkspacePreviewError(w, err)
		} else {
			writeRemoteWorkspaceError(w, err)
		}
		return
	}

	writeJSON(w, map[string]any{"meta": meta})
}

func (s *Server) handlePublicSharedFileContent(w http.ResponseWriter, r *http.Request, token string) {
	_, _, file, runtimeInfo, err := s.resolveSharedConversationFileRuntime(r.Context(), r, token, r.URL.Query().Get("fileId"))
	if err != nil {
		if writePublicShareRuntimeError(w, err) {
			return
		}
		writeError(w, "Shared file not found", http.StatusNotFound)
		return
	}

	textFile, err := s.readRuntimeSharedTextFile(r.Context(), runtimeInfo, file.OriginalPath, file.ID)
	if err != nil {
		if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
			return
		}
		if runtimeInfo.Mode == "local" {
			writeWorkspacePreviewError(w, err)
		} else {
			writeRemoteWorkspaceError(w, err)
		}
		return
	}

	writeJSON(w, map[string]any{
		"meta":      textFile.Meta,
		"content":   textFile.Content,
		"truncated": textFile.Truncated,
	})
}

func (s *Server) handlePublicSharedFileBuffer(w http.ResponseWriter, r *http.Request, token string) {
	_, _, file, runtimeInfo, err := s.resolveSharedConversationFileRuntime(r.Context(), r, token, r.URL.Query().Get("fileId"))
	if err != nil {
		if writePublicShareRuntimeError(w, err) {
			return
		}
		writeError(w, "Shared file not found", http.StatusNotFound)
		return
	}

	if err := s.serveRuntimeSharedBuffer(w, r, runtimeInfo, file.OriginalPath, file.ID); err != nil {
		if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
			return
		}
		if runtimeInfo.Mode == "local" {
			writeWorkspacePreviewError(w, err)
		} else {
			writeRemoteWorkspaceError(w, err)
		}
		return
	}
}

func (s *Server) handlePublicSharedHTMLPreview(w http.ResponseWriter, r *http.Request, token string) {
	fileID := r.URL.Query().Get("fileId")
	_, _, file, runtimeInfo, err := s.resolveSharedConversationFileRuntime(r.Context(), r, token, fileID)
	if err != nil {
		if writePublicShareRuntimeError(w, err) {
			return
		}
		writeError(w, "Shared file not found", http.StatusNotFound)
		return
	}

	textFile, err := s.readRuntimeSharedTextFile(r.Context(), runtimeInfo, file.OriginalPath, fileID)
	if err != nil {
		if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
			return
		}
		if runtimeInfo.Mode == "local" {
			writeWorkspacePreviewError(w, err)
		} else {
			writeRemoteWorkspaceError(w, err)
		}
		return
	}
	if textFile.Meta.PreviewKind != workspacepreview.PreviewKindHTML {
		writeError(w, "File does not support HTML preview", http.StatusUnsupportedMediaType)
		return
	}

	htmlContent := buildPublicShareHTMLPreviewDocument(token, fileID, textFile.Meta.Path, textFile.Content)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(htmlContent))
}

func (s *Server) handlePublicSharedHTMLAsset(w http.ResponseWriter, r *http.Request, token string, segments []string) {
	if len(segments) < 2 {
		writeError(w, "Invalid shared HTML asset path", http.StatusBadRequest)
		return
	}

	fileID, err := url.PathUnescape(segments[0])
	if err != nil || fileID == "" {
		writeError(w, "Invalid shared HTML asset path", http.StatusBadRequest)
		return
	}

	assetSegments := make([]string, 0, len(segments)-1)
	for _, raw := range segments[1:] {
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			writeError(w, "Invalid shared HTML asset path", http.StatusBadRequest)
			return
		}
		assetSegments = append(assetSegments, decoded)
	}
	if len(assetSegments) == 0 {
		writeError(w, "Invalid shared HTML asset path", http.StatusBadRequest)
		return
	}

	_, _, file, runtimeInfo, err := s.resolveSharedConversationFileRuntime(r.Context(), r, token, fileID)
	if err != nil {
		if writePublicShareRuntimeError(w, err) {
			return
		}
		writeError(w, "Shared file not found", http.StatusNotFound)
		return
	}

	scopedFilePath := file.OriginalPath
	if runtimeInfo.Mode == "local" {
		scopedFilePath = normalizeSharedWorkspacePath(runtimeInfo.WorkspacePath, file.OriginalPath)
	}

	baseDir := filepath.ToSlash(filepath.Dir(scopedFilePath))
	assetPath := strings.Join(assetSegments, "/")
	if assetSegments[0] == publicShareHTMLRootSentinel {
		assetPath = strings.Join(assetSegments[1:], "/")
	} else {
		assetPath = strings.Trim(strings.Join([]string{baseDir, assetPath}, "/"), "/")
	}
	if strings.TrimSpace(assetPath) == "" {
		writeError(w, "Invalid shared HTML asset path", http.StatusBadRequest)
		return
	}

	normalizedAssetPath := assetPath
	if runtimeInfo.Mode == "local" {
		resolvedAsset, err := s.workspaceSvc.ResolveFile(runtimeInfo.WorkspacePath, assetPath)
		if err != nil {
			writeWorkspacePreviewError(w, err)
			return
		}
		if resolvedAsset.Info.IsDir() {
			writeWorkspacePreviewError(w, workspacepreview.ErrIsDirectory)
			return
		}
		normalizedAssetPath = resolvedAsset.RelativePath
	}
	if !isWithinSharedHTMLDirectory(baseDir, normalizedAssetPath) {
		writeError(w, "Shared HTML asset not found", http.StatusNotFound)
		return
	}

	meta, err := s.statRuntimeSharedFile(r.Context(), runtimeInfo, normalizedAssetPath, normalizedAssetPath)
	if err != nil {
		if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
			return
		}
		if runtimeInfo.Mode == "local" {
			writeWorkspacePreviewError(w, err)
		} else {
			writeRemoteWorkspaceError(w, err)
		}
		return
	}

	if meta.PreviewKind == workspacepreview.PreviewKindHTML {
		textFile, err := s.readRuntimeSharedTextFile(r.Context(), runtimeInfo, normalizedAssetPath, normalizedAssetPath)
		if err != nil {
			if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
				return
			}
			if runtimeInfo.Mode == "local" {
				writeWorkspacePreviewError(w, err)
			} else {
				writeRemoteWorkspaceError(w, err)
			}
			return
		}

		htmlContent := buildPublicShareHTMLPreviewDocument(token, fileID, normalizedAssetPath, textFile.Content)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlContent))
		return
	}

	if strings.HasSuffix(strings.ToLower(meta.Name), ".css") || strings.Contains(meta.MIME, "text/css") {
		data, err := s.readRuntimeSharedTextFile(r.Context(), runtimeInfo, normalizedAssetPath, normalizedAssetPath)
		if err != nil {
			if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
				return
			}
			if runtimeInfo.Mode == "local" {
				writeWorkspacePreviewError(w, err)
			} else {
				writeRemoteWorkspaceError(w, err)
			}
			return
		}

		rootAssetBase := buildPublicShareHTMLAssetBaseURL(token, fileID, "", true)
		rewrittenCSS := rewriteCSSRootRelativeURLs(data.Content, rootAssetBase)

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Type", meta.MIME)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rewrittenCSS))
		return
	}

	if err := s.serveRuntimeSharedBuffer(w, r, runtimeInfo, normalizedAssetPath, normalizedAssetPath); err != nil {
		if writePublicShareRuntimeAccessError(w, runtimeInfo, err) {
			return
		}
		if runtimeInfo.Mode == "local" {
			writeWorkspacePreviewError(w, err)
		} else {
			writeRemoteWorkspaceError(w, err)
		}
	}
}

func writePublicShareRuntimeError(w http.ResponseWriter, err error) bool {
	var runtimeErr *sandbox.RuntimeError
	if errors.As(err, &runtimeErr) {
		writeError(w, "sandbox_unavailable", http.StatusServiceUnavailable)
		return true
	}
	return false
}

func writePublicShareRuntimeAccessError(w http.ResponseWriter, runtimeInfo ResolvedRuntime, err error) bool {
	if runtimeInfo.Mode != "sandbox" {
		return false
	}

	switch {
	case errors.Is(err, device.ErrDeviceNotFound),
		errors.Is(err, device.ErrDeviceOffline),
		errors.Is(err, device.ErrSetupNotReady),
		errors.Is(err, workspacepreview.ErrWorkspaceUnavailable):
		writeError(w, "sandbox_unavailable", http.StatusServiceUnavailable)
		return true
	}
	return false
}

func (s *Server) loadActiveSharedConversation(token string) (*storage.StoredShare, *storage.StoredSession, map[string]sharedConversationFile, error) {
	share := s.shareStore.GetActiveByToken(token)
	if share == nil {
		return nil, nil, nil, errors.New("share not found")
	}

	session, err := s.loadSessionSnapshot(share.ConversationID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !isSessionShareable(session) {
		return nil, nil, nil, errors.New("share not available")
	}

	workspaceID := session.WorkspaceID
	if workspaceID == "" {
		workspaceID = share.WorkspaceID
	}
	workspacePath := s.resolveWorkspacePath(workspaceID)

	return share, session, s.buildAllowedSharedConversationFileCatalog(share, workspacePath), nil
}

func (s *Server) resolveSharedConversationFile(token string, fileID string) (*storage.StoredShare, *storage.StoredSession, sharedConversationFile, string, error) {
	share, session, fileCatalog, err := s.loadActiveSharedConversation(token)
	if err != nil {
		return nil, nil, sharedConversationFile{}, "", err
	}

	file, ok := fileCatalog[fileID]
	if !ok {
		for _, candidate := range fileCatalog {
			if sharedConversationFileMatchesID(candidate, fileID) {
				file = candidate
				ok = true
				break
			}
		}
		if !ok {
			return nil, nil, sharedConversationFile{}, "", errors.New("shared file not found")
		}
	}

	workspaceID := session.WorkspaceID
	if workspaceID == "" {
		workspaceID = share.WorkspaceID
	}
	workspacePath := s.resolveWorkspacePath(workspaceID)

	return share, session, file, workspacePath, nil
}

func sharedConversationFileMatchesID(file sharedConversationFile, fileID string) bool {
	if file.ID == fileID {
		return true
	}

	legacyCandidates := []string{
		buildLegacySharedConversationFileID(file.OriginalPath, file.Name, file.Size),
		buildLegacySharedConversationFileID(file.OriginalPath, file.Name, 0),
		buildLegacySharedConversationFileID(file.ID, file.Name, file.Size),
	}

	for _, candidate := range legacyCandidates {
		if candidate == fileID {
			return true
		}
	}

	return false
}

func (s *Server) loadSessionSnapshot(conversationID string) (*storage.StoredSession, error) {
	session, err := s.sessionStore.Load(conversationID)
	if err == nil && session != nil {
		return session, nil
	}

	conv := s.conversations.Get(conversationID)
	if conv == nil {
		return nil, errors.New("session not found")
	}

	return &storage.StoredSession{
		ID:          conv.ID,
		Title:       storage.GenerateTitle(conv.Messages),
		Messages:    append([]conversation.Message(nil), conv.Messages...),
		ActiveAgent: conv.ActiveAgent,
		WorkspaceID: conv.WorkspaceID,
		CreatedAt:   conv.CreatedAt,
		UpdatedAt:   time.Now().UnixMilli(),
	}, nil
}

func isSessionShareable(session *storage.StoredSession) bool {
	hasUser := false
	hasAssistant := false

	for _, message := range session.Messages {
		switch message.Role {
		case "user":
			if strings.TrimSpace(message.Content) != "" || len(message.Files) > 0 {
				hasUser = true
			}
		case "assistant":
			if strings.TrimSpace(message.Content) != "" || message.ToolCall != nil {
				hasAssistant = true
			}
		}
	}

	return hasUser && hasAssistant
}

func generateShareToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return generateUUID()
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func toConversationShareResponse(share *storage.StoredShare) conversationShareResponse {
	files := make([]storage.StoredSharedFile, len(share.Files))
	copy(files, share.Files)
	return conversationShareResponse{
		ID:             share.ID,
		Token:          share.Token,
		ConversationID: share.ConversationID,
		Files:          files,
		CreatedAt:      share.CreatedAt,
		UpdatedAt:      share.UpdatedAt,
	}
}

func (s *Server) normalizeConversationShareFiles(workspacePath string, requested []storage.StoredSharedFile) ([]storage.StoredSharedFile, error) {
	files := make([]storage.StoredSharedFile, 0, len(requested))
	seen := make(map[string]bool)

	for _, requestedFile := range requested {
		path := strings.TrimSpace(requestedFile.Path)
		if path == "" {
			return nil, workspacepreview.ErrInvalidPath
		}
		if filepath.IsAbs(filepath.FromSlash(path)) {
			return nil, workspacepreview.ErrPathEscape
		}

		resolved, err := s.workspaceSvc.ResolveFile(workspacePath, path)
		if err != nil {
			return nil, err
		}
		if resolved.Info.IsDir() {
			return nil, workspacepreview.ErrIsDirectory
		}

		if seen[resolved.RelativePath] {
			continue
		}
		seen[resolved.RelativePath] = true
		files = append(files, storage.StoredSharedFile{Path: resolved.RelativePath})
	}

	return files, nil
}

func (s *Server) buildAllowedSharedConversationFileCatalog(share *storage.StoredShare, workspacePath string) map[string]sharedConversationFile {
	files := make(map[string]sharedConversationFile)

	for _, storedFile := range share.Files {
		path := strings.TrimSpace(storedFile.Path)
		if path == "" || filepath.IsAbs(filepath.FromSlash(path)) {
			continue
		}

		resolved, err := s.workspaceSvc.ResolveFile(workspacePath, path)
		if err != nil || resolved.Info.IsDir() {
			continue
		}

		messageFile := conversation.MessageFile{
			Name: resolved.Info.Name(),
			Path: resolved.RelativePath,
			Size: resolved.Info.Size(),
		}
		fileID := buildSharedConversationFileID(messageFile)
		if _, exists := files[fileID]; exists {
			continue
		}

		files[fileID] = sharedConversationFile{
			ID:           fileID,
			Name:         messageFile.Name,
			OriginalPath: messageFile.Path,
			Size:         messageFile.Size,
		}
	}

	return files
}

func buildPublicSharedMessages(session *storage.StoredSession, fileCatalog map[string]sharedConversationFile) []conversation.Message {
	messages := make([]conversation.Message, 0, len(session.Messages))

	for _, message := range session.Messages {
		copyMessage := message
		if len(message.Files) > 0 {
			files := make([]conversation.MessageFile, 0, len(message.Files))
			for _, file := range message.Files {
				fileID := buildSharedConversationFileID(file)
				sharedFile, ok := fileCatalog[fileID]
				if !ok {
					continue
				}
				files = append(files, conversation.MessageFile{
					Name: sharedFile.Name,
					Path: sharedFile.ID,
					Size: sharedFile.Size,
				})
			}
			copyMessage.Files = files
		}
		messages = append(messages, copyMessage)
	}

	return messages
}

func buildPublicSharedFileList(fileCatalog map[string]sharedConversationFile) []conversation.MessageFile {
	files := make([]conversation.MessageFile, 0, len(fileCatalog))
	for _, file := range fileCatalog {
		files = append(files, conversation.MessageFile{
			Name: file.Name,
			Path: file.ID,
			Size: file.Size,
		})
	}

	// Keep a stable order for the frontend rail.
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	return files
}

func buildSharedConversationFileCatalog(session *storage.StoredSession, workspacePath string) map[string]sharedConversationFile {
	files := make(map[string]sharedConversationFile)

	for _, message := range session.Messages {
		for _, file := range message.Files {
			file = enrichSharedFileMetadata(workspacePath, file)
			fileID := buildSharedConversationFileID(file)
			if _, exists := files[fileID]; exists {
				continue
			}

			files[fileID] = sharedConversationFile{
				ID:           fileID,
				Name:         file.Name,
				OriginalPath: file.Path,
				Size:         file.Size,
			}
		}

		if message.ToolCall != nil {
			for _, file := range extractToolCallFiles(message.ToolCall, workspacePath) {
				file = enrichSharedFileMetadata(workspacePath, file)
				fileID := buildSharedConversationFileID(file)
				if _, exists := files[fileID]; exists {
					continue
				}

				files[fileID] = sharedConversationFile{
					ID:           fileID,
					Name:         file.Name,
					OriginalPath: file.Path,
					Size:         file.Size,
				}
			}
		}
	}

	return files
}

func buildSharedConversationFileID(file conversation.MessageFile) string {
	sum := sha256.Sum256([]byte(file.Path + "\x00" + file.Name))
	return hex.EncodeToString(sum[:12])
}

func buildLegacySharedConversationFileID(path string, name string, size int64) string {
	sum := sha256.Sum256([]byte(path + "\x00" + name + "\x00" + strconv.FormatInt(size, 10)))
	return hex.EncodeToString(sum[:12])
}

func extractToolCallFiles(tool *conversation.ToolCallInfo, workspacePath string) []conversation.MessageFile {
	if tool == nil {
		return nil
	}

	allowlistedTools := map[string]bool{
		"Write":     true,
		"Read":      true,
		"Edit":      true,
		"MultiEdit": true,
	}
	if !allowlistedTools[tool.ToolName] {
		return nil
	}

	seen := make(map[string]bool)
	results := make([]conversation.MessageFile, 0, 1)

	appendCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true

		name := filepath.Base(candidate)
		size := int64(0)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			size = info.Size()
		}

		results = append(results, conversation.MessageFile{
			Name: name,
			Path: candidate,
			Size: size,
		})
	}

	if tool.Input != "" {
		appendCandidate(tool.Input)
	}

	if tool.RawInput != "" && tool.RawInput != "{}" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(tool.RawInput), &raw); err == nil {
			for _, key := range []string{"file_path", "path", "new_file_path"} {
				if value, ok := raw[key].(string); ok {
					appendCandidate(value)
				}
			}
		}
	}

	filtered := make([]conversation.MessageFile, 0, len(results))
	for _, file := range results {
		if file.Path == "" {
			continue
		}

		resolvedPath := file.Path
		if !filepath.IsAbs(resolvedPath) {
			resolvedPath = filepath.Join(workspacePath, resolvedPath)
		}
		resolvedPath = filepath.Clean(resolvedPath)

		if !strings.HasPrefix(resolvedPath, filepath.Clean(workspacePath)+string(filepath.Separator)) &&
			resolvedPath != filepath.Clean(workspacePath) {
			continue
		}

		file.Path = resolvedPath
		if info, err := os.Stat(resolvedPath); err == nil && !info.IsDir() {
			file.Size = info.Size()
			file.Name = info.Name()
		}
		filtered = append(filtered, file)
	}

	return filtered
}

func buildPublicShareHTMLPreviewDocument(token string, fileID string, relativePath string, content string) string {
	rootAssetBase := buildPublicShareHTMLAssetBaseURL(token, fileID, "", true)
	directoryBase := buildPublicShareHTMLAssetBaseURL(token, fileID, filepath.ToSlash(filepath.Dir(relativePath)), false)

	transformed := rewriteRootRelativeAttributes(content, rootAssetBase)
	transformed = rewriteStyleBlocks(transformed, rootAssetBase)
	transformed = rewriteStyleAttributes(transformed, rootAssetBase)

	injection := buildHTMLPreviewInjection(rootAssetBase, directoryBase)
	return injectIntoHTMLHead(transformed, injection)
}

func buildPublicShareHTMLAssetBaseURL(token string, fileID string, relativePath string, rooted bool) string {
	parts := []string{
		"/api/public/shares/conversations",
		url.PathEscape(token),
		"html-asset",
		url.PathEscape(fileID),
	}
	if rooted {
		parts = append(parts, publicShareHTMLRootSentinel)
	}

	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(relativePath)), "/")
	if trimmed != "" && trimmed != "." {
		for _, segment := range strings.Split(trimmed, "/") {
			if segment == "" {
				continue
			}
			parts = append(parts, url.PathEscape(segment))
		}
	}

	return strings.Join(parts, "/") + "/"
}

func isWithinSharedHTMLDirectory(baseDir string, candidate string) bool {
	base := filepath.Clean(filepath.FromSlash(baseDir))
	if base == "." {
		base = ""
	}

	target := filepath.Clean(filepath.FromSlash(candidate))
	if base == "" {
		return filepath.Dir(target) == "."
	}

	relative, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}

	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func normalizeSharedWorkspacePath(workspacePath string, originalPath string) string {
	if !filepath.IsAbs(originalPath) {
		return originalPath
	}

	relative, err := filepath.Rel(filepath.Clean(workspacePath), filepath.Clean(originalPath))
	if err != nil {
		return originalPath
	}
	if relative == "." {
		return relative
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return originalPath
	}

	return filepath.ToSlash(relative)
}

const maxSharedTextPreviewBytes = 1024 * 1024

func statSharedFile(workspacePath string, originalPath string, publicID string) (workspacepreview.FileMeta, string, error) {
	resolvedPath, info, err := resolveSharedFileOnDisk(workspacePath, originalPath)
	if err != nil {
		return workspacepreview.FileMeta{}, "", err
	}

	mimeType, err := detectSharedMimeType(resolvedPath, info.Name())
	if err != nil {
		return workspacepreview.FileMeta{}, "", err
	}

	return workspacepreview.FileMeta{
		Path:        publicID,
		Name:        info.Name(),
		Size:        info.Size(),
		ModifiedAt:  info.ModTime().UnixMilli(),
		MIME:        mimeType,
		PreviewKind: workspacepreview.DetectPreviewKind(info.Name()),
	}, resolvedPath, nil
}

func readSharedTextFile(workspacePath string, originalPath string, publicID string) (workspacepreview.TextFile, error) {
	meta, resolvedPath, err := statSharedFile(workspacePath, originalPath, publicID)
	if err != nil {
		return workspacepreview.TextFile{}, err
	}
	if meta.PreviewKind != workspacepreview.PreviewKindCode &&
		meta.PreviewKind != workspacepreview.PreviewKindMarkdown &&
		meta.PreviewKind != workspacepreview.PreviewKindHTML {
		return workspacepreview.TextFile{}, workspacepreview.ErrUnsupportedTextFile
	}

	fh, err := os.Open(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return workspacepreview.TextFile{}, workspacepreview.ErrNotFound
		}
		return workspacepreview.TextFile{}, err
	}
	defer fh.Close()

	data, err := io.ReadAll(io.LimitReader(fh, maxSharedTextPreviewBytes+1))
	if err != nil {
		return workspacepreview.TextFile{}, err
	}

	truncated := false
	if len(data) > maxSharedTextPreviewBytes {
		truncated = true
		data = data[:maxSharedTextPreviewBytes]
	}

	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return workspacepreview.TextFile{}, workspacepreview.ErrUnsupportedTextFile
	}

	return workspacepreview.TextFile{
		Meta:      meta,
		Content:   string(data),
		Truncated: truncated,
	}, nil
}

func resolveSharedFileOnDisk(workspacePath string, originalPath string) (string, os.FileInfo, error) {
	workspaceRoot, err := filepath.EvalSymlinks(filepath.Clean(workspacePath))
	if err != nil {
		return "", nil, workspacepreview.ErrWorkspaceUnavailable
	}

	candidate := originalPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspaceRoot, filepath.FromSlash(candidate))
	}

	resolvedPath, err := filepath.EvalSymlinks(filepath.Clean(candidate))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, workspacepreview.ErrNotFound
		}
		return "", nil, err
	}

	relative, err := filepath.Rel(workspaceRoot, resolvedPath)
	if err != nil {
		return "", nil, workspacepreview.ErrPathEscape
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, workspacepreview.ErrPathEscape
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, workspacepreview.ErrNotFound
		}
		return "", nil, err
	}
	if info.IsDir() {
		return "", nil, workspacepreview.ErrIsDirectory
	}

	return resolvedPath, info, nil
}

func detectSharedMimeType(path string, name string) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		if strings.HasPrefix(mimeType, "text/") && !strings.Contains(mimeType, "charset=") {
			return mimeType + "; charset=utf-8", nil
		}
		return mimeType, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return "", err
	}

	mimeType := http.DetectContentType(header[:n])
	if strings.HasPrefix(mimeType, "text/") && !strings.Contains(mimeType, "charset=") {
		return mimeType + "; charset=utf-8", nil
	}

	return mimeType, nil
}

func enrichSharedFileMetadata(workspacePath string, file conversation.MessageFile) conversation.MessageFile {
	resolvedPath, info, err := resolveSharedFileOnDisk(workspacePath, file.Path)
	if err != nil {
		return file
	}

	file.Path = resolvedPath
	file.Name = info.Name()
	file.Size = info.Size()
	return file
}
