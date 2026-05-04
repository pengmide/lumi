package main

import (
	"testing"

	"github.com/pengmide/lumi/internal/agent"
	"github.com/pengmide/lumi/internal/config"
)

func newTestRunner() *Runner {
	cfg := &ExecutorConfig{
		DeviceID:     "dev-1",
		DefaultAgent: "claude",
		Agents: []config.AgentConfig{
			{ID: "claude", Name: "Claude Code", Command: "echo"},
		},
	}
	client := NewClient("http://example.com", "token", cfg)
	return client.runner
}

func TestAbortCurrentTaskClearsRunnerState(t *testing.T) {
	t.Parallel()

	runner := newTestRunner()
	proc := agent.NewProcess(&config.AgentConfig{
		ID:      "claude",
		Name:    "Claude Code",
		Command: "echo",
	})

	runner.agents["claude"] = proc
	runner.initialized["claude"] = true
	runner.sessions["task-1"] = "session-1"
	runner.currentTask = &runningTask{
		TaskID:    "task-1",
		AgentID:   "claude",
		SessionID: "session-1",
		Process:   proc,
	}

	runner.AbortCurrentTask("connection lost")

	if running := runner.RunningTaskIDs(); len(running) != 0 {
		t.Fatalf("RunningTaskIDs() = %v, want empty", running)
	}
	if got := runner.sessionForTask("task-1"); got != "" {
		t.Fatalf("sessionForTask(task-1) = %q, want empty", got)
	}
	if _, ok := runner.agents["claude"]; ok {
		t.Fatal("agent process still cached after AbortCurrentTask")
	}
	if runner.initialized["claude"] {
		t.Fatal("agent initialization state still cached after AbortCurrentTask")
	}
}

func TestFinishTaskKeepsNewerTaskRegistered(t *testing.T) {
	t.Parallel()

	runner := newTestRunner()
	runner.client.setSetupReady(true)
	runner.sessions["task-old"] = "session-old"
	runner.currentTask = &runningTask{TaskID: "task-new"}

	runner.finishTask("task-old")

	if running := runner.RunningTaskIDs(); len(running) != 1 || running[0] != "task-new" {
		t.Fatalf("RunningTaskIDs() = %v, want [task-new]", running)
	}
	if got := runner.sessionForTask("task-old"); got != "" {
		t.Fatalf("sessionForTask(task-old) = %q, want empty", got)
	}
}
