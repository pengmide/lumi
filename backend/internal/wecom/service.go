package wecom

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/pengmide/lumi/internal/config"
)

type Status struct {
	Running         bool   `json:"running"`
	Configured      bool   `json:"configured"`
	ConfigError     string `json:"configError,omitempty"`
	LastError       string `json:"lastError,omitempty"`
	LastConnectedAt int64  `json:"lastConnectedAt,omitempty"`
	LastMessageAt   int64  `json:"lastMessageAt,omitempty"`
}

type Service struct {
	config       *config.Config
	runner       ChatRunner
	configStore  *ConfigStore
	runtimeStore *RuntimeStore
	convStore    *ConversationStore

	mu            sync.RWMutex
	running       bool
	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
	locks         *conversationLocks
}

func NewService(cfg *config.Config, runner ChatRunner) *Service {
	svc := &Service{
		config:       cfg,
		runner:       runner,
		configStore:  NewConfigStore(),
		runtimeStore: NewRuntimeStore(),
		convStore:    NewConversationStore(),
		locks:        newConversationLocks(),
	}
	if state, err := svc.runtimeStore.Load(); err == nil && state.Running {
		state.Running = false
		_ = svc.runtimeStore.Save(state)
	}
	return svc
}

func (s *Service) AutoStartIfEnabled() {
	cfg, err := s.configStore.Load()
	if err != nil || !cfg.Enabled {
		return
	}
	if err := s.Start(); err != nil {
		_ = s.updateRuntime(func(state *RuntimeState) {
			state.LastError = err.Error()
			state.Running = false
		})
	}
}

func (s *Service) Start() error {
	cfg, err := s.configStore.Load()
	if err != nil {
		return err
	}
	if err := s.validateConfigForRuntime(cfg); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.running = true
	s.monitorCancel = cancel
	s.monitorDone = done
	if err := s.updateRuntime(func(state *RuntimeState) {
		state.Running = true
		state.LastError = ""
	}); err != nil {
		s.running = false
		s.monitorCancel = nil
		s.monitorDone = nil
		cancel()
		return err
	}

	go s.runWebSocketLoop(ctx, normalizeConfig(cfg), done)
	return nil
}

func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return s.updateRuntime(func(state *RuntimeState) {
			state.Running = false
		})
	}
	cancel := s.monitorCancel
	done := s.monitorDone
	s.monitorCancel = nil
	s.monitorDone = nil
	s.running = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Service) GetStatus() (Status, error) {
	cfg, err := s.configStore.Load()
	if err != nil {
		return Status{}, err
	}
	state, err := s.runtimeStore.Load()
	if err != nil {
		return Status{}, err
	}

	status := Status{
		Running:         s.IsRunning(),
		LastError:       state.LastError,
		LastConnectedAt: state.LastConnectedAt,
		LastMessageAt:   state.LastMessageAt,
	}
	if err := s.validateConfigForRuntime(cfg); err != nil {
		status.Configured = false
		status.ConfigError = err.Error()
		return status, nil
	}
	status.Configured = true
	return status, nil
}

func (s *Service) TestConnection(ctx context.Context) error {
	cfg, err := s.configStore.Load()
	if err != nil {
		return err
	}
	if err := s.validateConfigForRuntime(cfg); err != nil {
		return err
	}
	runtime := newWebSocketRuntime(s, normalizeConfig(cfg))
	return runtime.TestConnection(ctx)
}

func (s *Service) SaveConfig(_ context.Context, next Config) (SanitizedConfig, error) {
	next = normalizeConfig(next)
	if err := s.validateConfigForSave(next); err != nil {
		return SanitizedConfig{}, err
	}
	if err := s.configStore.Save(next); err != nil {
		return SanitizedConfig{}, err
	}
	return SanitizeConfig(next), nil
}

func (s *Service) Enable() error {
	cfg, err := s.configStore.Load()
	if err != nil {
		return err
	}
	if err := s.validateConfigForRuntime(cfg); err != nil {
		return err
	}

	cfg.Enabled = true
	if err := s.configStore.Save(cfg); err != nil {
		return err
	}
	if err := s.Start(); err != nil {
		cfg.Enabled = false
		_ = s.configStore.Save(cfg)
		return err
	}
	return nil
}

func (s *Service) Disable() error {
	cfg, err := s.configStore.Load()
	if err != nil {
		return err
	}
	if err := s.Stop(); err != nil {
		return err
	}
	cfg.Enabled = false
	return s.configStore.Save(cfg)
}

func (s *Service) validateConfigForSave(cfg Config) error {
	if cfg.Mode != defaultMode {
		return errors.New("mode must be websocket")
	}
	if strings.TrimSpace(cfg.WorkspaceID) == "" {
		return errors.New("workspaceId is required")
	}
	workspace := s.config.FindWorkspace(cfg.WorkspaceID)
	if workspace == nil {
		return errors.New("workspace not found")
	}
	if workspace.Kind != "" && workspace.Kind != "local" {
		return errors.New("workspace must be local")
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		return errors.New("agentId is required")
	}
	if s.config.FindAgent(cfg.AgentID) == nil {
		return errors.New("agent not found")
	}
	if cfg.ConnectTimeoutMs < 1000 {
		return errors.New("connectTimeoutMs must be at least 1000")
	}
	if cfg.HeartbeatIntervalMs < 5000 {
		return errors.New("heartbeatIntervalMs must be at least 5000")
	}
	if cfg.MessageAckTimeoutMs < 1000 {
		return errors.New("messageAckTimeoutMs must be at least 1000")
	}
	return nil
}

func (s *Service) validateConfigForRuntime(cfg Config) error {
	if err := s.validateConfigForSave(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.BotID) == "" {
		return errors.New("botId is required")
	}
	if strings.TrimSpace(cfg.BotSecret) == "" {
		return errors.New("botSecret is required")
	}
	return nil
}

func (s *Service) updateRuntime(update func(*RuntimeState)) error {
	state, err := s.runtimeStore.Load()
	if err != nil {
		return err
	}
	update(&state)
	return s.runtimeStore.Save(state)
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
