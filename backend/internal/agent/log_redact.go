package agent

import (
	"regexp"
	"strings"
)

var secretValuePattern = regexp.MustCompile(`(?i)(sk-[a-z0-9][a-z0-9_-]{4,})`)

func redactLogValue(value string) string {
	redacted := secretValuePattern.ReplaceAllString(value, "sk-<redacted>")
	for _, key := range []string{
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"API_KEY",
		"TOKEN",
		"SECRET",
	} {
		redacted = redactEnvAssignment(redacted, key)
	}
	return redacted
}

func redactEnvAssignment(value string, key string) string {
	idx := strings.Index(strings.ToUpper(value), key+"=")
	if idx < 0 {
		return value
	}

	start := idx + len(key) + 1
	end := start
	for end < len(value) {
		switch value[end] {
		case ' ', '\n', '\r', '\t', '"', '\'':
			return value[:start] + "<redacted>" + value[end:]
		default:
			end++
		}
	}
	return value[:start] + "<redacted>"
}

func isSensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "API_KEY") ||
		strings.HasSuffix(upper, "_KEY")
}
