package proc_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scarb/skope/internal/proc"
)

func TestRunnerTerminatesDescendantsAfterCancellationOrParentExit(t *testing.T) {
	for _, scenario := range []string{"cancel", "timeout", "output", "tree-exit", "tree-no-pipes"} {
		t.Run(scenario, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			mode := scenario
			runner := proc.Runner{Timeout: 8 * time.Second, OutputLimit: 1024}
			switch scenario {
			case "cancel":
				mode = "tree"
			case "timeout":
				mode = "tree"
				runner.Timeout = 2 * time.Second
			case "output":
				mode = "tree-output"
			}
			req := helperRequest(t, mode, listener.Addr().String(), filepath.Join(t.TempDir(), "ready"))
			if systemRoot, ok := os.LookupEnv("SystemRoot"); ok {
				req.Env = append(req.Env, "SystemRoot="+systemRoot)
			}
			go func() {
				result, err := runner.Run(ctx, req)
				if err != nil {
					err = fmt.Errorf("%w (helper stderr: %s)", err, result.Stderr)
				}
				done <- err
			}()
			conn, err := listener.Accept()
			if err != nil {
				cancel()
				t.Fatalf("accept: %v; runner: %v", err, <-done)
			}
			defer conn.Close()
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var ready [1]byte
			if _, err := io.ReadFull(conn, ready[:]); err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				switch scenario {
				case "cancel":
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error = %v", err)
					}
				case "timeout":
					var timeoutErr *proc.TimeoutError
					if !errors.As(err, &timeoutErr) {
						t.Fatalf("error = %v", err)
					}
				case "output":
					var limitErr *proc.OutputLimitError
					if !errors.As(err, &limitErr) {
						t.Fatalf("error = %v", err)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Run did not return after termination")
			}
			if n, err := conn.Read(ready[:]); n != 0 || !isClosedTreeConnection(err) {
				t.Fatalf("descendant still holds connection: n=%d err=%v", n, err)
			}
		})
	}
}
