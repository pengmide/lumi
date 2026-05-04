package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndSavePublicServerURL(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "lumi.config.json")
	original := `{
  "publicServerURL": "https://chat.example.com/lumi",
  "agents": [
    {
      "id": "claude",
      "name": "Claude Code",
      "command": "npx"
    }
  ],
  "defaultAgent": "claude"
}
`
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PublicServerURL != "https://chat.example.com/lumi" {
		t.Fatalf("cfg.PublicServerURL = %q, want saved value", cfg.PublicServerURL)
	}

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `"publicServerURL": "https://chat.example.com/lumi"`) {
		t.Fatalf("saved config missing publicServerURL: %s", data)
	}
}
