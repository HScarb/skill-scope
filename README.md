# skope

Session-scoped skill whitelisting for coding agents.
`skope claude -s <skill-set>` and `skope codex -s <skill-set>` launch agents
with a session skill whitelist, without changing persistent agent settings.
Claude also supports legacy commands and eligible foreign skill projection;
Codex controls existing native paths and local installed plugins in place.

**Status:** pre-alpha. Phase 3 implementation is undergoing final verification.
Linux/macOS support launching Claude and Codex. Windows supports executable-based
probes and dry-run; production process inspection, final handoff, and command
script resolution remain Phase 6. OpenCode and interactive management remain
Phases 4 and 5. See the verification log for tested versions and platforms.

## Usage

Create `~/.skope/skillsets.toml` (or use `SKOPE_HOME` to select another directory):

```toml
version = 1

[skillsets.dev]
skills = ["code-review", "team-check"]
bundled = false

[skillsets.dev.plugins]
claude = ["my-tools@my-marketplace"]
codex = ["my-plugin@my-marketplace"]
```

For example, `code-review` can live at `~/.claude/skills/code-review/SKILL.md`
and `team-check` at `~/.agents/skills/team-check/SKILL.md`. For `plugins.claude`,
use an installed ID from `claude plugin list --json`. For `plugins.codex`, use a
local installed `plugin@marketplace` ID from Codex config/cache as described below.
Omit either agent entry when it is not needed.
Allowing a plugin enables the whole plugin. Selecting a skill ID alone does not
enable its plugin; plugin dependencies may be enabled by Claude itself.
Claude may also install a missing plugin that the allowlist enables.

```sh
skope list
skope claude -s dev
skope claude -s dev --dry-run
skope claude -s none
skope help claude
skope codex -s dev
skope codex -s dev --dry-run
skope codex -s none
skope help codex
```

Place skope options before agent arguments; `--` explicitly starts passthrough.
Skill sets can be combined with `-s dev,other`. An explicit set is currently
required even in an interactive terminal; `none` keeps the target agent's normal
configuration. The interactive selector is planned for Phase 5.

For an isolated launch, skope rejects `--settings`, `--setting-sources`,
`--plugin-dir`, `--plugin-url`, and `--add-dir` in configured or passed-through
Claude arguments. `-s none` bypasses these isolation checks.

## Codex isolation

Codex support is verified against Linux CLI **0.153.1** and its local config/cache
layout. Put ordinary skills in `~/.agents/skills/<id>/SKILL.md` or a supported
Codex root. Skill IDs use directory basenames, including plugin skills;
frontmatter names and Codex plugin display namespaces do not replace these IDs.
`skope list` lists configured sets; use matching directory IDs in `skills`. `plugins.codex` uses installed
`plugin@marketplace` IDs and enables every skill in each allowed plugin. An
ordinary skill selection never enables its plugin. Missing plugins are reported;
skope does not install them.

An active launch emits explicit `skills.config` true/false entries for every
scanned ordinary and installed-plugin canonical `SKILL.md` path, plus plugin
switches. Selected paths recover from existing User path/name denies. Selecting
all paths still emits explicit `true` entries; an empty selection disables every
known path. Only an empty inventory emits `skills.config=[]`. Paths sharing one
canonical file cannot be separated: allowing any alias allows that file and
produces a warning. Codex does not project foreign skills or Claude commands;
the summary distinguishes unavailable entries from missing IDs.

`bundled=true` allows the bundled source while preserving individual User system
skill denies; `false` disables that source. Active isolation also sets
`features.remote_plugin=false`. This is a verified local-source boundary, not
verification of authenticated remote plugin caches or account policy. Scanning
is not an atomic snapshot; skills added after scanning can escape the denylist.

For active sets, configured and passthrough arguments cannot change cwd/profile,
skills/plugins/source roots, marketplaces, or the protected remote feature.
This includes parent tables and `--enable/--disable remote_plugin`. Diagnostics
identify the option without exposing its value. `none` preserves normal Codex
arguments and configuration. Put skope options before agent arguments and use
`--` to begin passthrough.

Codex dry-run scans local files without invoking Codex, writing configuration,
projecting files, or creating a session. A launch stores only `owner.json` in its
session; the next launch or dry-run reaps completed sessions. Windows scanning
uses the real Known Folder home; changing HOME/USERPROFILE does not redirect it.
Automated Windows active binary tests therefore skip before production discovery,
while fixture-injected application/filesystem tests and none/help tests execute.
Nonempty `CODEX_HOME` preserves literal whitespace and must be absolute; a
whitespace-only value is rejected. Active discovery rejects parent-directory
(`..`) components in the platform home or `CODEX_HOME`, since cleaning them
before following symlinks can select a different directory.
Unix CI explicitly checks that active binary integration passes without skipping.

## Projection and previews

Claude can project eligible ordinary Codex skills from global, cwd-to-repository
project, and Unix admin roots, including `~/.agents/skills` and `$CODEX_HOME/skills`
(default `~/.codex/skills`). Locally installed Codex plugin skills are enumerated
with their plugin identity retained; they are reported as unavailable/plugin-only
for Claude and cannot be projected, regardless of whether the plugin is allowed. Complete non-target Claude plugin enumeration and authenticated
remote plugin management remain Phase 5. Native Claude entries take precedence;
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

For Claude with an active skill set, `--dry-run` executes one plugin-list probe
and performs source and projection checks. `-s none --dry-run` only previews the passthrough
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
