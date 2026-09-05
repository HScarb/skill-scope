//go:build !windows

package session_test

import (
	"errors"
	"io"
)

func lockDeletion(string) (io.Closer, error) {
	return nil, errors.New("windows-only deletion lock called on another platform")
}
