package wechat

import (
	"context"
	"encoding/json"
	"net/http"
)

func (s *Service) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/wechat/config" && r.Method == http.MethodGet:
		s.handleGetConfig(w, r)
	case r.URL.Path == "/api/wechat/config" && r.Method == http.MethodPost:
		s.handleSaveConfig(w, r)
	case r.URL.Path == "/api/wechat/status" && r.Method == http.MethodGet:
		s.handleStatus(w, r)
	case r.URL.Path == "/api/wechat/test" && r.Method == http.MethodPost:
		s.handleTest(w, r)
	case r.URL.Path == "/api/wechat/enable" && r.Method == http.MethodPost:
		s.handleEnable(w, r)
	case r.URL.Path == "/api/wechat/disable" && r.Method == http.MethodPost:
		s.handleDisable(w, r)
	case r.URL.Path == "/api/wechat/login/start" && r.Method == http.MethodPost:
		s.handleLoginStart(w, r)
	case r.URL.Path == "/api/wechat/login/events" && r.Method == http.MethodGet:
		s.handleLoginEvents(w, r)
	default:
		writeError(w, "Not found", http.StatusNotFound)
	}
}

func (s *Service) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.configStore.Load()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, SanitizeConfig(cfg))
}

func (s *Service) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Enabled     bool    `json:"enabled"`
		LoginMode   string  `json:"loginMode"`
		AccountID   string  `json:"accountId"`
		BotToken    *string `json:"botToken"`
		BaseURL     string  `json:"baseUrl"`
		WorkspaceID string  `json:"workspaceId"`
		AgentID     string  `json:"agentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	current, err := s.configStore.Load()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	next := current
	next.Enabled = data.Enabled
	next.LoginMode = data.LoginMode
	next.AccountID = data.AccountID
	next.BaseURL = data.BaseURL
	next.WorkspaceID = data.WorkspaceID
	next.AgentID = data.AgentID
	if data.BotToken != nil {
		next.BotToken = *data.BotToken
	}

	sanitized, err := s.SaveConfig(r.Context(), next)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"success": true,
		"config":  sanitized,
	})
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.GetStatus()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, status)
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), apiTimeout)
	defer cancel()
	if err := s.TestConnection(ctx); err != nil {
		writeJSON(w, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, map[string]any{
		"success": true,
		"message": "connection ok",
	})
}

func (s *Service) handleEnable(w http.ResponseWriter, r *http.Request) {
	if err := s.Enable(); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleDisable(w http.ResponseWriter, r *http.Request) {
	if err := s.Disable(); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"loginId": s.login.Start()})
}

func (s *Service) handleLoginEvents(w http.ResponseWriter, r *http.Request) {
	loginID := r.URL.Query().Get("id")
	s.login.ServeEvents(w, r, loginID)
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
