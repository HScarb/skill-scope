package termsafe

import (
	"strings"
	"unicode/utf8"
)

// EnvValue hides sensitive environment values and escapes other values for display.
func EnvValue(key, value string) string {
	key = strings.ToLower(key)
	if key == "opencode_config_content" {
		return "<redacted>"
	}
	for _, word := range []string{"token", "key", "secret", "password", "auth", "header", "credential"} {
		if strings.Contains(key, word) {
			return "<redacted>"
		}
	}
	return Escape(value)
}

// StderrExcerpt escapes at most 2048 input bytes without splitting a UTF-8 rune.
func StderrExcerpt(raw []byte) string {
	const limit = 2048
	if len(raw) <= limit {
		return Escape(string(raw))
	}
	prefix := raw[:limit]
	end := 0
	for end < len(prefix) && utf8.FullRune(prefix[end:]) {
		_, size := utf8.DecodeRune(prefix[end:])
		end += size
	}
	return Escape(string(prefix[:end])) + " [truncated]"
}
