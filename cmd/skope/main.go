// Command skope launches claude, codex and opencode with a
// session-scoped skill whitelist.
package main

import (
	"os"

	"github.com/scarb/skope/internal/cli"
)

// version is overridden at build time via
// -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr, version))
}
