package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/pengmide/lumi/internal/conversation"
	"github.com/pengmide/lumi/internal/device"
	"github.com/pengmide/lumi/internal/storage"
	workspacepreview "github.com/pengmide/lumi/internal/workspace"
)

func (s *Server) normalizeConversationShareFilesForRuntime(ctx context.Context, runtimeInfo ResolvedRuntime, requested []storage.StoredSharedFile) ([]storage.StoredSharedFile, error) {
	files := make([]storage.StoredSharedFile, 0, len(requested))
	seen := make(map[string]bool)

	for _, requestedFile := range requested {
		path := requestedFile.Path
		if path == "" {
			return nil, workspacepreview.ErrInvalidPath
		}

		normalizedPath, err := s.normalizeRuntimeSharePath(ctx, runtimeInfo, path)
		if err != nil {
			return nil, err
		}
		if seen[normalizedPath] {
			continue
		}
		seen[normalizedPath] = true
		files = append(files, storage.StoredSharedFile{Path: normalizedPath})
	}

	return files, nil
}

func (s *Server) loadActiveSharedConversationRuntime(ctx context.Context, req *http.Request, token string) (*storage.StoredShare, *storage.StoredSession, map[string]sharedConversationFile, ResolvedRuntime, error) {
	share := s.shareStore.GetActiveByToken(token)
	if share == nil {
		return nil, nil, nil, ResolvedRuntime{}, errors.New("share not found")
	}

	session, err := s.loadSessionSnapshot(share.ConversationID)
	if err != nil {
		return nil, nil, nil, ResolvedRuntime{}, err
	}
	if !isSessionShareable(session) {
		return nil, nil, nil, ResolvedRuntime{}, errors.New("share not available")
	}

	workspaceID := session.WorkspaceID
	if workspaceID == "" {
		workspaceID = share.WorkspaceID
	}
	runtimeInfo, err := s.resolveWorkspaceRuntime(ctx, workspaceID, req)
	if err != nil {
		return nil, nil, nil, ResolvedRuntime{}, err
	}

	return share, session, s.buildAllowedSharedConversationFileCatalogForRuntime(ctx, runtimeInfo, share), runtimeInfo, nil
}

func (s *Server) resolveSharedConversationFileRuntime(ctx context.Context, req *http.Request, token string, fileID string) (*storage.StoredShare, *storage.StoredSession, sharedConversationFile, ResolvedRuntime, error) {
	share, session, fileCatalog, runtimeInfo, err := s.loadActiveSharedConversationRuntime(ctx, req, token)
	if err != nil {
		return nil, nil, sharedConversationFile{}, ResolvedRuntime{}, err
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
			return nil, nil, sharedConversationFile{}, ResolvedRuntime{}, errors.New("shared file not found")
		}
	}

	return share, session, file, runtimeInfo, nil
}

func (s *Server) buildAllowedSharedConversationFileCatalogForRuntime(ctx context.Context, runtimeInfo ResolvedRuntime, share *storage.StoredShare) map[string]sharedConversationFile {
	files := make(map[string]sharedConversationFile)

	for _, storedFile := range share.Files {
		path := storedFile.Path
		if path == "" {
			continue
		}

		meta, err := s.statRuntimeSharedFile(ctx, runtimeInfo, path, path)
		if err != nil {
			continue
		}

		messageFile := conversation.MessageFile{
			Name: meta.Name,
			Path: meta.Path,
			Size: meta.Size,
		}
		fileID := buildSharedConversationFileID(messageFile)
		if _, exists := files[fileID]; exists {
			continue
		}

		files[fileID] = sharedConversationFile{
			ID:           fileID,
			Name:         meta.Name,
			OriginalPath: meta.Path,
			Size:         meta.Size,
		}
	}

	return files
}

func (s *Server) normalizeRuntimeSharePath(ctx context.Context, runtimeInfo ResolvedRuntime, path string) (string, error) {
	if runtimeInfo.Mode == "local" {
		resolved, err := s.workspaceSvc.ResolveFile(runtimeInfo.WorkspacePath, path)
		if err != nil {
			return "", err
		}
		if resolved.Info.IsDir() {
			return "", workspacepreview.ErrIsDirectory
		}
		return resolved.RelativePath, nil
	}

	var response struct {
		Meta workspacepreview.FileMeta `json:"meta"`
	}
	if err := s.deviceWorkspacePayload(ctx, runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceMeta, device.WorkspaceRequestPayload{
		Path: path,
	}, &response); err != nil {
		return "", err
	}
	return response.Meta.Path, nil
}

func (s *Server) statRuntimeSharedFile(ctx context.Context, runtimeInfo ResolvedRuntime, originalPath string, publicID string) (workspacepreview.FileMeta, error) {
	if runtimeInfo.Mode == "local" {
		meta, _, err := statSharedFile(runtimeInfo.WorkspacePath, originalPath, publicID)
		return meta, err
	}

	var response struct {
		Meta workspacepreview.FileMeta `json:"meta"`
	}
	if err := s.deviceWorkspacePayload(ctx, runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceMeta, device.WorkspaceRequestPayload{
		Path: originalPath,
	}, &response); err != nil {
		return workspacepreview.FileMeta{}, err
	}
	meta := response.Meta
	meta.Path = publicID
	s.touchResolvedRuntime(runtimeInfo)
	return meta, nil
}

func (s *Server) readRuntimeSharedTextFile(ctx context.Context, runtimeInfo ResolvedRuntime, originalPath string, publicID string) (workspacepreview.TextFile, error) {
	if runtimeInfo.Mode == "local" {
		return readSharedTextFile(runtimeInfo.WorkspacePath, originalPath, publicID)
	}

	var textFile workspacepreview.TextFile
	if err := s.deviceWorkspacePayload(ctx, runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceText, device.WorkspaceRequestPayload{
		Path: originalPath,
	}, &textFile); err != nil {
		return workspacepreview.TextFile{}, err
	}
	textFile.Meta.Path = publicID
	s.touchResolvedRuntime(runtimeInfo)
	return textFile, nil
}

func (s *Server) serveRuntimeSharedBuffer(w http.ResponseWriter, r *http.Request, runtimeInfo ResolvedRuntime, originalPath string, publicID string) error {
	if runtimeInfo.Mode == "local" {
		meta, resolvedPath, err := statSharedFile(runtimeInfo.WorkspacePath, originalPath, publicID)
		if err != nil {
			return err
		}
		fileHandle, err := os.Open(resolvedPath)
		if err != nil {
			return err
		}
		defer fileHandle.Close()

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if meta.MIME != "" {
			w.Header().Set("Content-Type", meta.MIME)
		}
		http.ServeContent(w, r, meta.Name, time.UnixMilli(meta.ModifiedAt), fileHandle)
		return nil
	}

	meta, data, err := s.deviceWorkspaceBuffer(r.Context(), runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, originalPath)
	if err != nil {
		return err
	}
	meta.Path = publicID
	s.touchResolvedRuntime(runtimeInfo)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if meta.MIME != "" {
		w.Header().Set("Content-Type", meta.MIME)
	}
	http.ServeContent(w, r, meta.Name, time.UnixMilli(meta.ModifiedAt), bytes.NewReader(data))
	return nil
}
