// Package termsafe prepares untrusted text for terminal display.
package termsafe

import (
	"fmt"
	"strings"
)

// Escape replaces terminal controls and directional formatting with visible escapes.
func Escape(value string) string {
	var escaped strings.Builder
	for _, r := range value {
		switch {
		case r <= 0x1f || r >= 0x7f && r <= 0x9f:
			fmt.Fprintf(&escaped, `\x%02x`, r)
		case r == 0x061c || r == 0x200e || r == 0x200f ||
			r >= 0x2028 && r <= 0x202e || r >= 0x2066 && r <= 0x2069:
			fmt.Fprintf(&escaped, `\u%04x`, r)
		default:
			escaped.WriteRune(r)
		}
	}
	return escaped.String()
}
