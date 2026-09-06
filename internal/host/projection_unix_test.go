//go:build !windows

package host_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/scarb/skope/internal/host"
	"golang.org/x/sys/unix"
)

func TestProjectionRootSpecialFilesNeverBlock(t *testing.T) {
	if os.Getenv("SKOPE_TEST_PROJECTION_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProjectionRootSpecialFilesNeverBlock$", "-test.v")
		cmd.Env = append(os.Environ(), "SKOPE_TEST_PROJECTION_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("special-file helper: %v (context %v)\n%s", err, ctx.Err(), output)
		}
		return
	}
	for _, kind := range []string{"fifo", "socket"} {
		t.Run(kind, func(t *testing.T) {
			// t.TempDir includes this long test name, exceeding macOS's Unix
			// socket path limit under its default temporary directory.
			dir, err := os.MkdirTemp("", "skope-proj-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(dir); err != nil {
					t.Error(err)
				}
			})
			name := filepath.Join(dir, kind)
			if kind == "fifo" {
				if err := unix.Mkfifo(name, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				listener, err := net.Listen("unix", name)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := listener.Close(); err != nil {
						t.Error(err)
					}
				}()
			}
			_, reject, err := projectionInspector().Inspect(context.Background(), dir)
			if err != nil || reject == nil || reject.Reason != "special-file" {
				t.Fatalf("reject=%+v err=%v", reject, err)
			}
			if kind == "fifo" {
				r, err := host.OpenProjectionRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				f, err := r.Open(kind)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
