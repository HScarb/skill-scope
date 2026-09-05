// Package proc runs short auxiliary commands with bounded output and lifetime.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultTimeout           = 15 * time.Second
	DefaultOutputLimit int64 = 4 << 20
	stderrPrefixLimit        = 2 << 10
)

type Request struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
}

type Result struct{ Stdout, Stderr []byte }

type Runner struct {
	Timeout     time.Duration
	OutputLimit int64
}

type TimeoutError struct {
	Command string
	Timeout time.Duration
	Stderr  string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s: auxiliary command timed out after %s%s", e.Command, e.Timeout, stderrSuffix(e.Stderr))
}
func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }

type OutputLimitError struct {
	Command string
	Stream  string
	Limit   int64
	Stderr  string
}

func (e *OutputLimitError) Error() string {
	return fmt.Sprintf("%s: %s exceeds %d bytes%s", e.Command, e.Stream, e.Limit, stderrSuffix(e.Stderr))
}

type ExitError struct {
	Command string
	Code    int
	Stderr  string
	Cause   error
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s: auxiliary command exited with code %d%s", e.Command, e.Code, stderrSuffix(e.Stderr))
}
func (e *ExitError) Unwrap() error { return e.Cause }

func stderrSuffix(stderr string) string {
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

// Run requires an explicitly supplied environment; an empty slice means no
// inherited environment. Executable must already have been resolved by the caller.
func (r Runner) Run(ctx context.Context, req Request) (Result, error) {
	if r.Timeout < 0 || r.OutputLimit < 0 {
		return Result{}, errors.New("auxiliary command timeout and output limit must not be negative")
	}
	if req.Env == nil {
		return Result{}, errors.New("auxiliary command environment must be explicitly supplied")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if r.Timeout == 0 {
		r.Timeout = DefaultTimeout
	}
	if r.OutputLimit == 0 {
		r.OutputLimit = DefaultOutputLimit
	}
	req.Args = append([]string(nil), req.Args...)
	env := make([]string, len(req.Env))
	copy(env, req.Env)
	req.Env = env
	command := filepath.Base(req.Executable)
	started := time.Now()
	deadline := started.Add(r.Timeout)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return Result{}, fmt.Errorf("%s: stdout pipe: %w", command, err)
	}
	defer stdoutR.Close()
	defer stdoutW.Close()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return Result{}, fmt.Errorf("%s: stderr pipe: %w", command, err)
	}
	defer stderrR.Close()
	defer stderrW.Close()
	p, err := startProcess(req, stdoutW, stderrW)
	if err != nil {
		return Result{}, fmt.Errorf("%s: start auxiliary command: %w", command, err)
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()

	reads := make(chan streamResult, 2)
	go func() { reads <- readBounded(stdoutR, "stdout", r.OutputLimit) }()
	go func() { reads <- readBounded(stderrR, "stderr", r.OutputLimit) }()
	waits := make(chan error, 1)
	go func() { waits <- p.wait() }()
	var result Result
	var cause, waitErr, cleanupErr error
	killed := false
	terminate := func(closeReaders bool) {
		if !killed {
			cleanupErr = p.kill()
			killed = true
		}
		if closeReaders {
			_ = stdoutR.Close()
			_ = stderrR.Close()
		}
	}
	done := ctx.Done()
	// Wait and both readers must finish before returning, including when the
	// parent exits while descendants still hold the inherited output handles.
	for remaining := 3; remaining > 0; {
		select {
		case waitErr = <-waits:
			waits = nil
			remaining--
			terminate(false)
		case read := <-reads:
			remaining--
			if read.stream == "stdout" {
				result.Stdout = read.data
			} else {
				result.Stderr = read.data
			}
			if read.overflow && cause == nil {
				cause = &OutputLimitError{Command: command, Stream: read.stream, Limit: r.OutputLimit}
				terminate(true)
			} else if read.err != nil && cause == nil {
				cause = fmt.Errorf("%s: read %s: %w", command, read.stream, read.err)
				terminate(true)
			}
		case <-done:
			done = nil
			if cause == nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					cause = &TimeoutError{Command: command, Timeout: deadline.Sub(started)}
				} else {
					cause = fmt.Errorf("%s: auxiliary command: %w", command, ctx.Err())
				}
			}
			terminate(true)
		}
	}
	prefix := string(result.Stderr[:min(len(result.Stderr), stderrPrefixLimit)])
	var timeoutErr *TimeoutError
	var limitErr *OutputLimitError
	switch {
	case errors.As(cause, &timeoutErr):
		timeoutErr.Stderr = prefix
	case errors.As(cause, &limitErr):
		limitErr.Stderr = prefix
	case cause == nil && waitErr != nil:
		cause = &ExitError{Command: command, Code: p.exitCode(), Stderr: prefix, Cause: waitErr}
	}
	return result, errors.Join(cause, cleanupErr)
}

type streamResult struct {
	stream   string
	data     []byte
	overflow bool
	err      error
}

func readBounded(reader io.Reader, stream string, limit int64) streamResult {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, io.LimitReader(reader, limit))
	result := streamResult{stream: stream, data: buf.Bytes(), err: err}
	if err == nil {
		var extra [1]byte
		n, readErr := reader.Read(extra[:])
		result.overflow = n != 0
		if !errors.Is(readErr, io.EOF) {
			result.err = readErr
		}
	}
	return result
}
