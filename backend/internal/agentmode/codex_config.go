package agentmode

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var codexSandboxModePattern = regexp.MustCompile(`(?m)^\s*sandbox_mode\s*=.*$`)

func writeCodexSandboxMode(sandboxMode string) error {
	path := codexConfigPath()
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	existing := string(content)
	newline := "\n"
	if strings.Contains(existing, "\r\n") {
		newline = "\r\n"
	}

	sandboxLine := `sandbox_mode = "` + sandboxMode + `"`
	var next string
	switch {
	case existing == "":
		next = sandboxLine + newline
	case codexSandboxModePattern.MatchString(existing):
		next = codexSandboxModePattern.ReplaceAllString(existing, sandboxLine)
	default:
		trimmed := strings.TrimRight(existing, "\r\n\t ")
		next = trimmed + newline + sandboxLine + newline
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(next), 0644)
}
