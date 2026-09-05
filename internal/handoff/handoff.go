// Package handoff transfers execution to a coding agent on supported platforms.
package handoff

import "errors"

var ErrUnsupported = errors.New("handoff is not implemented on this platform")

type Handoff struct{}
