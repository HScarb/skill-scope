package termsafe_test

import (
	"fmt"
	"testing"

	"github.com/scarb/skope/internal/termsafe"
)

func TestEscapePreservesTextAndEscapesTerminalControls(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"中文 path", "中文 path"},
		{`C:\path\file`, `C:\path\file`},
		{"a\n\tb", `a\x0a\x09b`},
		{"\x1b]0;title\a", `\x1b]0;title\x07`},
		{"\u009b31m\u202e", `\x9b31m\u202e`},
		{"\u061c\u200e\u200f\u2028\u2029", `\u061c\u200e\u200f\u2028\u2029`},
		{string([]byte{0xff, 0x9b, 0x1b, 0xc0, 0x8a}), "��\\x1b��"},
	}
	for _, tc := range cases {
		if got := termsafe.Escape(tc.in); got != tc.want {
			t.Errorf("Escape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeEscapesEveryRestrictedRune(t *testing.T) {
	ranges := []struct{ first, last rune }{
		{0x00, 0x1f}, {0x7f, 0x9f}, {0x061c, 0x061c},
		{0x200e, 0x200f}, {0x2028, 0x202e}, {0x2066, 0x2069},
	}
	for _, span := range ranges {
		for r := span.first; r <= span.last; r++ {
			t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
				want := fmt.Sprintf(`\u%04x`, r)
				if r <= 0xff {
					want = fmt.Sprintf(`\x%02x`, r)
				}
				if got := termsafe.Escape(string(r)); got != want {
					t.Errorf("Escape(%q) = %q, want %q", r, got, want)
				}
			})
		}
	}
}
