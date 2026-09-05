//go:build !windows

package proc_test

import (
	"errors"
	"io"
	"os/exec"
)

func isClosedTreeConnection(err error) bool { return errors.Is(err, io.EOF) }

func configureTreeChild(*exec.Cmd) {}
