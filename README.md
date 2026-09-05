# skope

Session-scoped skill whitelisting for coding agents.
`skope claude -s <skill-set>` launches Claude with the selected native
skills and legacy commands enabled, without changing persistent agent settings.

**Status:** pre-alpha. Phase 1 implementation and real-Claude validation are complete.
Production handoff currently supports Linux/macOS and Claude native skills/commands.
Plugins, bundled-skill controls, projection, conflict-flag detection, terminal-safe
output, Codex/OpenCode adapters and Windows handoff remain planned for later phases.
See the design spec, §14, and the verification log for validation scope.

## Usage

Create `~/.skope/skillsets.toml` (or use `SKOPE_HOME` to select another directory):

```toml
version = 1

[skillsets.dev]
skills = ["code-review"]
```

```sh
skope list
skope claude -s dev
skope claude -s dev --dry-run
skope claude -s none
skope help claude
```

Place skope options before Claude arguments; `--` explicitly starts passthrough.
Skill sets can be combined with `-s dev,other`. Phase 1 requires an explicit set
even in an interactive terminal; `none` keeps Claude's normal configuration.

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
