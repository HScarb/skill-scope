package handoff

import "errors"

var ErrUnsupported = errors.New("handoff is not implemented on this platform")

type Handoff struct{}
