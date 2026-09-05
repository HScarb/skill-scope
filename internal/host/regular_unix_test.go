//go:build !windows

package host_test

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/scarb/skope/internal/host"
	"golang.org/x/sys/unix"
)

func TestOpenRegularSpecialFiles(t *testing.T) {
	if os.Getenv("SKOPE_TEST_REGULAR_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenRegularSpecialFiles$", "-test.v")
		cmd.Env = append(os.Environ(), "SKOPE_TEST_REGULAR_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("special-file helper: %v (context %v)\n%s", err, ctx.Err(), output)
		}
		return
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	}()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{fifo, socket, "/dev/null", link} {
		file, err := (host.OSFileSystem{}).OpenRegular(name)
		if file != nil || !errors.Is(err, host.ErrNotRegular) {
			t.Fatalf("OpenRegular(%s)=%v,%v", name, file, err)
		}
	}
}
