# skope

Session-scoped skill whitelisting for coding agents.
`skope claude -s <skill-set>` launches Claude with selected native skills,
legacy commands, plugins, bundled skills, and eligible foreign skills, without
changing persistent agent settings.

**Status:** pre-alpha. Phase 2 implementation and verification are complete.
Linux/macOS support launching Claude. Windows supports executable-based probes
and dry-run; production session process inspection, final handoff, and command
script resolution remain part of Phase 6. Codex/OpenCode adapters and interactive
management remain later phases. See the verification log for tested versions and
platforms.

## Usage

Create `~/.skope/skillsets.toml` (or use `SKOPE_HOME` to select another directory):

```toml
version = 1

[skillsets.dev]
skills = ["code-review", "team-check"]
bundled = false

[skillsets.dev.plugins]
claude = ["my-tools@my-marketplace"]
```

For example, `code-review` can live at `~/.claude/skills/code-review/SKILL.md`
and `team-check` at `~/.agents/skills/team-check/SKILL.md`. Replace the plugin ID
with an installed ID from `claude plugin list --json`, or omit the plugins table.
Allowing a plugin enables the whole plugin. Selecting a skill ID alone does not
enable its plugin; plugin dependencies may be enabled by Claude itself.
Claude may also install a missing plugin that the allowlist enables.

```sh
skope list
skope claude -s dev
skope claude -s dev --dry-run
skope claude -s none
skope help claude
```

Place skope options before Claude arguments; `--` explicitly starts passthrough.
Skill sets can be combined with `-s dev,other`. An explicit set is currently
required even in an interactive terminal; `none` keeps Claude's normal
configuration. The interactive selector is planned for Phase 5.

For an isolated launch, skope rejects `--settings`, `--setting-sources`,
`--plugin-dir`, `--plugin-url`, and `--add-dir` in configured or passed-through
Claude arguments. `-s none` bypasses these isolation checks.

## Projection and previews

Phase 2 considers two foreign global sources: `~/.agents/skills` and
`$CODEX_HOME/skills` (default `~/.codex/skills`). Other agents' project, admin,
and plugin sources remain later work. Native Claude entries take precedence;
eligible foreign skills are copied into the temporary session and loaded with
`--add-dir`.

- Each projected skill is limited to 2,000 files and 20 MiB. Internal links are
  expanded; external links, cycles, special files, and plugin manifests are
  rejected. Text references to external paths produce warnings.
- Target names conservatively reject case aliases such as `Foo`/`foo` on every
  platform. The first valid selected candidate keeps the target. Files use
  exclusive creation and never overwrite an existing projection target.
- Copied files use mode `0600` and directories `0700`, including scripts, which
  do not retain an executable bit. Windows uses the user directory's inherited
  DACL; these modes are not a separate Windows ACL guarantee.
- Sources are checked again during copying. These checks are not a filesystem
  snapshot and cannot guarantee detection of changes after the final check.

With an active skill set, `--dry-run` executes one plugin-list probe and performs
source and projection checks. `-s none --dry-run` only previews the passthrough
launch. Both may reap finished sessions, but create no new session. The preview
shows the final arguments, generated settings, and relative session file paths;
it omits copied file bodies and the inherited environment. Displayed environment
changes are redacted where necessary, and external text is escaped for terminals.

If Claude suppresses project plugins because the workspace is untrusted, the
incomplete inventory stops the isolated launch.
Complete the workspace trust step in Claude independently, then retry.

On Windows, configure the actual Claude executable for probes and dry-run:

```toml
# ~/.skope/config.toml
version = 1

[agents.claude]
command = 'D:\path\to\claude.exe'
```

`.cmd` and `.bat` launchers are not resolved in this phase.

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
