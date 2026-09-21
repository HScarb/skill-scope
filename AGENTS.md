# Repository Guidelines

## Project Structure & Module Organization

`skope` is a Go 1.24 CLI for launching coding agents with a session-scoped skill whitelist.

- `cmd/skope/main.go` is the only production `main` package. Keep it limited to version injection, `cli.Execute`, and `os.Exit`.
- `internal/cli/` owns the Cobra command tree and delegates business logic to lower-level packages.
- `internal/agent/claude/` and `internal/agent/codex/` own target-specific adapters; keep target behavior in these packages and keep the shared `internal/agent/` interfaces stable.
- `internal/testutil/` contains test-only helpers; `fakeagent/` records argv, environment, and working directory for integration tests.
- `docs/superpowers/specs/` is the implementation contract. Add phase-level execution plans under `docs/superpowers/plans/`.

Keep application code under `internal/`; this repository does not expose a public Go library. Follow the dependency rules enforced by `depguard` in `.golangci.yml`: packages must not import `internal/cli` or `internal/launch` upward, agent adapters must remain independent, and leaf packages must not import `internal/agent`.

## Build, Test, and Development Commands

```sh
go run ./cmd/skope --help
go run ./cmd/skope version
go build ./...
go test ./...
go test -short ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...
```

Use the short test command for a fast pass that skips building the fake agent. Run `go test -race ./...` on Linux or macOS with cgo available; the Windows development environment does not provide the required C toolchain.

## Coding Style & Naming Conventions

Run `gofmt` and `goimports` before committing. Use tabs as emitted by `gofmt`, lowercase package names, `CamelCase` exported identifiers, and `camelCase` internal identifiers. Define interfaces in the consuming package. Avoid `init()` registration; construct adapter registries explicitly in `internal/cli`. Keep comments factual and explain non-obvious constraints.

## Testing Guidelines

Use the standard `testing` package and black-box packages named `xxx_test`. Name tests `TestBehaviorUnderCondition`; prefer table-driven cases for parsers and validation. Put fixtures and golden files in package-local `testdata/`. Integration tests should use `testutil.BuildFakeAgent`, temporary directories, and redirected agent configuration, never real user settings. The design target is at least 80% coverage.

## Commit & Pull Request Guidelines

History follows Conventional Commits, for example `feat: add version command`, `test: add BuildFakeAgent helper`, and `chore: initialize Go module`. Keep each commit focused and use an imperative summary.

Pull requests must describe the behavior change, link the relevant spec or issue, list verification commands, and call out platform-specific behavior. Include terminal output samples when CLI text or flows change; screenshots are unnecessary for non-visual changes.
