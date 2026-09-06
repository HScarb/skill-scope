package termsafe_test

import (
	"strings"
	"testing"

	"github.com/scarb/skope/internal/termsafe"
)

func TestEnvValueRedactsSensitiveKeys(t *testing.T) {
	keys := []string{
		"API_TOKEN", "api_key", "Client_Secret", "PASSWORD", "Authorization",
		"Extra_Headers", "cloud_CREDENTIALS", "monkey", "OPENCODE_CONFIG_CONTENT",
		"opencode_config_content",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			if got := termsafe.EnvValue(key, "sensitive\x1b[31m"); got != "<redacted>" {
				t.Errorf("EnvValue(%q) = %q, want <redacted>", key, got)
			}
		})
	}
}

func TestEnvValueEscapesOtherValues(t *testing.T) {
	for _, key := range []string{"", "HOME", "CODEX_HOME"} {
		if got := termsafe.EnvValue(key, "中文\n\x1b"); got != `中文\x0a\x1b` {
			t.Errorf("EnvValue(%q) = %q", key, got)
		}
	}
}

func TestStderrExcerptTruncatesBeforeEscaping(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"exact limit", strings.Repeat("a", 2048), strings.Repeat("a", 2048)},
		{"over limit", strings.Repeat("a", 2049), strings.Repeat("a", 2048) + " [truncated]"},
		{"controls", "a\x00\x1b[31m\n", `a\x00\x1b[31m\x0a`},
		{"escaped limit", strings.Repeat("\x1b", 2049), strings.Repeat(`\x1b`, 2048) + " [truncated]"},
		{"whole rune at limit", strings.Repeat("a", 2045) + "中x", strings.Repeat("a", 2045) + "中 [truncated]"},
		{"three byte rune across limit", strings.Repeat("a", 2047) + "中文", strings.Repeat("a", 2047) + " [truncated]"},
		{"four byte rune across limit", strings.Repeat("a", 2046) + "😀x", strings.Repeat("a", 2046) + " [truncated]"},
		{"two byte rune across limit", strings.Repeat("a", 2047) + "éx", strings.Repeat("a", 2047) + " [truncated]"},
		{"invalid UTF8", "\xff\x9b\x1b\xc0\x8a", "��\\x1b��"},
		{"incomplete untruncated UTF8", "\xe4\xb8", "��"},
		{"invalid byte at limit", strings.Repeat("a", 2047) + "\xffx", strings.Repeat("a", 2047) + "� [truncated]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := termsafe.StderrExcerpt([]byte(tc.raw)); got != tc.want {
				t.Errorf("StderrExcerpt() = %q, want %q", got, tc.want)
			}
		})
	}
}
