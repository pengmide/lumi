package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/pengmide/lumi/internal/config"
	"github.com/pengmide/lumi/internal/device"
	workspacepreview "github.com/pengmide/lumi/internal/workspace"
)

type remoteWorkspaceBuffer struct {
	Meta    workspacepreview.FileMeta `json:"meta"`
	Content string                    `json:"content"`
}

func (s *Server) remoteWorkspacePayload(ctx context.Context, ws config.WorkspaceConfig, typ device.MessageType, payload device.WorkspaceRequestPayload, out any) error {
	if !isRemoteWorkspaceConfig(ws) {
		return errors.New("workspace is not remote")
	}
	workspacePath := ws.RemotePath
	if workspacePath == "" {
		workspacePath = ws.Path
	}
	return s.deviceWorkspacePayload(ctx, ws.DeviceID, workspacePath, typ, payload, out)
}

func (s *Server) remoteWorkspaceBuffer(ctx context.Context, ws config.WorkspaceConfig, relativePath string) (workspacepreview.FileMeta, []byte, error) {
	workspacePath := ws.RemotePath
	if workspacePath == "" {
		workspacePath = ws.Path
	}
	return s.deviceWorkspaceBuffer(ctx, ws.DeviceID, workspacePath, relativePath)
}

func remoteWorkspaceError(err error) error {
	var wsErr device.WorkspaceError
	if errors.As(err, &wsErr) {
		switch wsErr.Code {
		case "invalid_path", "path_escape":
			return workspacepreview.ErrInvalidPath
		case "not_found":
			return workspacepreview.ErrNotFound
		case "is_directory":
			return workspacepreview.ErrIsDirectory
		case "unsupported", "too_large":
			return workspacepreview.ErrUnsupportedTextFile
		case "unavailable":
			return workspacepreview.ErrWorkspaceUnavailable
		}
	}
	if errors.Is(err, device.ErrDeviceNotFound) || errors.Is(err, device.ErrDeviceOffline) || errors.Is(err, device.ErrSetupNotReady) {
		return err
	}
	return err
}

func writeRemoteWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, device.ErrDeviceNotFound):
		writeError(w, "Device not found", http.StatusNotFound)
	case errors.Is(err, device.ErrDeviceOffline):
		writeError(w, "Device is offline", http.StatusConflict)
	case errors.Is(err, device.ErrSetupNotReady):
		writeError(w, "Device setup is not ready", http.StatusConflict)
	default:
		writeWorkspacePreviewError(w, err)
	}
}
