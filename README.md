# skope

Session-scoped skill whitelisting for `claude`, `codex` and `opencode`.
`skope <agent> -s <skill-set>` launches the agent with only the selected
skills, plugins and bundled skills visible, without touching the agent's
persistent configuration.

**Status:** pre-alpha. Phase 0 (project scaffolding) complete; no agent
launching yet. See the phase plan in the design spec, §14.

## Design

- Spec: [`docs/superpowers/specs/2026-09-02-skill-scope-design.md`](docs/superpowers/specs/2026-09-02-skill-scope-design.md)
- Plans: [`docs/superpowers/plans/`](docs/superpowers/plans/)
- Real-agent verification log: [`docs/verification.md`](docs/verification.md)

## Development

Requires Go 1.24+.
On Windows, run make from Git Bash.

```sh
make check          # gofmt, go vet, golangci-lint, go test, go build
make test-race      # needs cgo; runs in CI on Linux/macOS
make build          # ./skope (or skope.exe)
```

`golangci-lint` and `goreleaser` run via `go run` at pinned versions;
no global install needed.

## Layout

See spec §11. All code lives under `internal/`; `cmd/skope/main.go` is
the only `main` package. Dependency direction between packages is
enforced by `depguard` in `.golangci.yml`.
