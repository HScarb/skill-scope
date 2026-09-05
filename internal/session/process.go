package session

import "errors"

var ErrProcessNotFound = errors.New("process not found")
var ErrUnsupported = errors.New("process inspection unsupported")

type ProcessInspector interface {
	StartToken(pid int) (string, error)
}

type OSProcessInspector struct{}
