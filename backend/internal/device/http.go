package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pengmide/lumi/internal/setupcheck"
)

type DeviceDTO struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Alias          string                  `json:"alias,omitempty"`
	Hidden         bool                    `json:"hidden,omitempty"`
	DisplayName    string                  `json:"displayName"`
	Status         string                  `json:"status"`
	SetupReady     bool                    `json:"setupReady"`
	SetupStatus    *setupcheck.SetupStatus `json:"setupStatus,omitempty"`
	DefaultAgentID string                  `json:"defaultAgentId,omitempty"`
	Agents         []DeviceAgentInfo       `json:"agents,omitempty"`
	WorkspaceID    string                  `json:"workspaceId,omitempty"`
	Version        string                  `json:"version,omitempty"`
	LastHeartbeat  int64                   `json:"lastHeartbeat"`
	RegisteredAt   int64                   `json:"registeredAt"`
	UpdatedAt      int64                   `json:"updatedAt"`
	RunningTaskIDs []string                `json:"runningTaskIds,omitempty"`
}

type listDevicesResponse struct {
	Devices []DeviceDTO `json:"devices"`
}

type pairingCommandResponse struct {
	Command    string `json:"command"`
	Server     string `json:"server"`
	ConfigPath string `json:"configPath"`
}

type updateDeviceAliasRequest struct {
	Alias string `json:"alias"`
}

type updateDeviceAliasResponse struct {
	Device DeviceDTO `json:"device"`
}

type successResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

func (r *Registry) HandleListDevices(w http.ResponseWriter, req *http.Request) {
	devices := r.ListDevices()
	response := listDevicesResponse{Devices: make([]DeviceDTO, 0, len(devices))}
	for _, device := range devices {
		if device.Hidden {
			continue
		}
		response.Devices = append(response.Devices, toDeviceDTO(device))
	}
	writeJSON(w, response)
}

func (r *Registry) HandlePairingCommand(w http.ResponseWriter, server string) {
	response := pairingCommandResponse{
		Command:    "device-executor connect --server " + server + " --token " + r.secret,
		Server:     server,
		ConfigPath: "~/.device-executor/config.json",
	}
	writeJSON(w, response)
}

func (r *Registry) HandleUpdateAlias(w http.ResponseWriter, req *http.Request, deviceID string) {
	if device, ok := r.GetDevice(deviceID); ok && device.Hidden {
		writeError(w, "Device not found", http.StatusNotFound)
		return
	}

	var data updateDeviceAliasRequest
	if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	alias := strings.TrimSpace(data.Alias)
	if len(alias) > 100 {
		writeError(w, "Alias must be 100 characters or fewer", http.StatusBadRequest)
		return
	}

	device, err := r.UpdateAlias(deviceID, alias)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			writeError(w, "Device not found", http.StatusNotFound)
			return
		}
		writeError(w, "Failed to update device alias", http.StatusInternalServerError)
		return
	}

	writeJSON(w, updateDeviceAliasResponse{Device: toDeviceDTO(device)})
}

func (r *Registry) HandleDeleteDevice(w http.ResponseWriter, req *http.Request, deviceID string) {
	if device, ok := r.GetDevice(deviceID); ok && device.Hidden {
		writeError(w, "Device not found", http.StatusNotFound)
		return
	}

	err := r.DeleteDevice(deviceID)
	switch {
	case errors.Is(err, ErrDeviceNotFound):
		writeError(w, "Device not found", http.StatusNotFound)
	case errors.Is(err, ErrDeviceBusy):
		writeError(w, "Device is busy", http.StatusConflict)
	case err != nil:
		writeError(w, "Failed to delete device", http.StatusInternalServerError)
	default:
		writeJSON(w, successResponse{Success: true})
	}
}

func (r *Registry) HandleRequestSetupCheck(w http.ResponseWriter, req *http.Request, deviceID string) {
	device, ok := r.GetDevice(deviceID)
	if !ok {
		writeError(w, "Device not found", http.StatusNotFound)
		return
	}
	if device.Hidden {
		writeError(w, "Device not found", http.StatusNotFound)
		return
	}
	if device.Status == StatusOffline {
		writeError(w, "Device is offline", http.StatusConflict)
		return
	}

	if err := r.SendToDevice(req.Context(), deviceID, MsgSetupCheck, "", SetupCheckPayload{}); err != nil {
		if errors.Is(err, ErrDeviceOffline) {
			writeError(w, "Device is offline", http.StatusConflict)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(successResponse{
		Success: true,
		Message: "Setup check requested",
	})
}

func toDeviceDTO(device Device) DeviceDTO {
	displayName := device.Name
	if strings.TrimSpace(device.Alias) != "" {
		displayName = device.Alias
	}

	return DeviceDTO{
		ID:             device.ID,
		Name:           device.Name,
		Alias:          device.Alias,
		Hidden:         device.Hidden,
		DisplayName:    displayName,
		Status:         device.Status,
		SetupReady:     device.SetupReady,
		SetupStatus:    device.SetupStatus,
		DefaultAgentID: device.DefaultAgentID,
		Agents:         append([]DeviceAgentInfo(nil), device.Agents...),
		WorkspaceID:    device.WorkspaceID,
		Version:        device.Version,
		LastHeartbeat:  device.LastHeartbeat,
		RegisteredAt:   device.RegisteredAt,
		UpdatedAt:      device.UpdatedAt,
		RunningTaskIDs: append([]string(nil), device.RunningTaskIDs...),
	}
}

func inferServerURL(req *http.Request) string {
	if req == nil {
		return "http://localhost:3000"
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(req.Host)
	if host == "" {
		host = "localhost:3000"
	}
	return scheme + "://" + host
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func newMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
