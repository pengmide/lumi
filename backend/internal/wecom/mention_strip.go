package wecom

import "strings"

func stripWeComAtMentions(s string, botIDs ...string) string {
	s = strings.TrimSpace(s)
	for _, id := range botIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		s = stripOneWeComAtMention(s, id)
		s = strings.TrimSpace(s)
	}
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func stripOneWeComAtMention(s, botID string) string {
	if s == "" || botID == "" {
		return s
	}
	s = removeAllEqualFold(s, "＠"+botID)
	needleLower := "@" + strings.ToLower(botID)
	for {
		lower := strings.ToLower(s)
		idx := strings.Index(lower, needleLower)
		if idx < 0 {
			return s
		}
		end := idx + len(needleLower)
		if end > len(s) {
			return s
		}
		s = s[:idx] + s[end:]
	}
}

func removeAllEqualFold(s, sub string) string {
	if sub == "" {
		return s
	}
	subLower := strings.ToLower(sub)
	for {
		lower := strings.ToLower(s)
		idx := strings.Index(lower, subLower)
		if idx < 0 {
			return s
		}
		s = s[:idx] + s[idx+len(sub):]
	}
}
