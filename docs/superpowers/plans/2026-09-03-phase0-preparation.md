# skill-scope Phase 0（项目准备）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Phase 1 开工前把仓库、Go 模块、CI、lint、构建入口、fake agent 测试基础设施和验证文档骨架全部就位，使 Phase 1 的第一个业务测试可以直接开写。

**Architecture:** Phase 0 不实现任何业务逻辑。它建立 spec §11 的目录骨架中「与业务无关」的部分：`cmd/skope/main.go`、`internal/cli` 的 cobra 根命令与 `version`、`internal/testutil/fakeagent` 及其编译辅助、`.golangci.yml`（含 §11.1 depguard 规则）、GitHub Actions 三平台 CI、goreleaser 骨架、`docs/verification.md` 模板。每个包只放让「工具链跑通」所需的最小代码，不预写 Phase 1 的类型。

**Tech Stack:** Go 1.24+（本机 go1.27.1）、spf13/cobra v1.10.x、golangci-lint v2、GitHub Actions、goreleaser v2。

**Spec 对应：** `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §2、§11、§12、§14.7。spec §14 没有单列 Phase 0；本 plan 定义它为「Phase 1 之前的项目准备」，Task 13 会把这一定义回写到 spec。

**执行状态：** 2026-09-04 本地验收与 GitHub Actions 三平台 CI 全部通过，Phase 0 已关闭。

---

## 前置事实（执行前必读）

- **仓库当前状态：** `D:\workspace\vibe\skill-scope` 下只有 `.gitignore`、`docs/`、`.claude/`。**没有 `.git` 目录**，需要重新 `git init`。`.claude/worktrees/` 是旧 agent 残留，不纳入版本控制。
- **本机 Windows 无 gcc，`go test -race` 不可用**（`-race` 需要 cgo）。本地校验用 `go test ./...`；`-race` 只在 CI 的 ubuntu/macos job 上跑。Task 13 把这一点回写到 spec §14.7。
- **golangci-lint 本机未安装。** Task 7 会用 `go run` 方式调用固定版本，不要求全局安装。
- 模块路径固定为 `github.com/scarb/skope`（spec §11）。
- 提交信息用 conventional commits，不加 Co-Authored-By（用户全局设置已禁用署名）。
- 所有命令在 Git Bash 下运行，路径用正斜杠。

## 文件结构

Phase 0 结束时仓库应为：

```
skope/
├── .github/workflows/ci.yml          # Task 8
├── .gitignore                        # 已存在，Task 1 追加
├── .golangci.yml                     # Task 7
├── .goreleaser.yaml                  # Task 9
├── Makefile                          # Task 10
├── README.md                         # Task 11
├── cmd/skope/main.go                 # Task 3
├── docs/
│   ├── superpowers/...               # 已存在
│   └── verification.md               # Task 12
├── go.mod / go.sum                   # Task 2
└── internal/
    ├── cli/
    │   ├── root.go                   # Task 3：cobra 根命令、Execute 入口
    │   ├── root_test.go              # Task 3
    │   ├── version.go                # Task 4
    │   └── version_test.go           # Task 4
    └── testutil/
        ├── fakeagent/main.go         # Task 5：记录 argv/env/cwd 后按指定码退出
        ├── build.go                  # Task 6：把 fakeagent 编译到临时目录
        └── build_test.go             # Task 6
```

每个文件的职责：

| 文件 | 职责 |
|---|---|
| `cmd/skope/main.go` | 唯一 main 包。持有 ldflags 注入的 `version` 变量，调用 `cli.Execute`，把返回的退出码交给 `os.Exit`。不含其他逻辑 |
| `internal/cli/root.go` | 构造 cobra 根命令；`Execute(args, stdout, stderr, version) int` 是全部测试的入口 seam |
| `internal/cli/version.go` | `skope version` 子命令，打印版本字符串 |
| `internal/testutil/fakeagent/main.go` | 独立 main 包。启动后把 argv、env、cwd 写成 JSON 到 `FAKEAGENT_OUT` 指定的文件，然后以 `FAKEAGENT_EXIT` 指定的退出码退出 |
| `internal/testutil/build.go` | `BuildFakeAgent(t testing.TB) string`：在 `t.TempDir()` 中 `go build` fakeagent，返回可执行文件路径 |

---

### Task 1: 初始化 git 仓库并提交已有文档

**Files:**
- Modify: `.gitignore`
- Existing: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`、`docs/superpowers/plans/2026-09-03-phase0-preparation.md`

- [ ] **Step 1: 确认当前目录没有 `.git`，并初始化**

Run:
```bash
cd D:/workspace/vibe/skill-scope && ls -a && git init -b main
```
Expected: 输出含 `Initialized empty Git repository`。若 `ls -a` 显示已有 `.git`，跳过 `git init`。

- [ ] **Step 2: 追加 `.gitignore` 规则**

把 `.gitignore` 改为以下完整内容（保留原有 7 行并追加 `.claude/` 与 coverage 产物）：

```gitignore
/skope
/skope.exe
/dist/
/.worktrees/
/.idea/
/.vscode/
*.test
*.out
/.claude/
/coverage.txt
```

- [ ] **Step 3: 确认 `.claude/` 被忽略**

Run:
```bash
cd D:/workspace/vibe/skill-scope && git status --short
```
Expected: 只列出 `.gitignore` 与 `docs/`，不出现 `.claude/`。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add .gitignore docs && git commit -m "docs: add design spec and phase 0 plan"
```

---

### Task 2: 初始化 Go 模块

**Files:**
- Create: `go.mod`

- [ ] **Step 1: 初始化模块**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go mod init github.com/scarb/skope
```
Expected: `go: creating new go.mod: module github.com/scarb/skope`

- [ ] **Step 2: 把 go 指令固定为 1.24**

`go mod init` 会写入本机版本（1.27.x）。spec §2 要求 Go 1.24+，CI 矩阵用 1.24。用以下命令改写：

```bash
cd D:/workspace/vibe/skill-scope && go mod edit -go=1.24 && cat go.mod
```
Expected `go.mod` 内容：
```
module github.com/scarb/skope

go 1.24
```

- [ ] **Step 3: 验证空模块可构建**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go build ./... && echo BUILD_OK
```
Expected: `BUILD_OK`（当前没有 Go 文件，`go build` 无输出且成功）。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add go.mod && git commit -m "chore: initialize Go module github.com/scarb/skope"
```

---

### Task 3: cobra 根命令与 `Execute` seam

**Files:**
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`
- Create: `cmd/skope/main.go`

`Execute` 是本项目所有 CLI 测试的入口。它接收 argv、输出流、版本字符串，返回退出码，不直接调用 `os.Exit`、不读 `os.Args`，这样测试可以完全控制输入输出。

- [ ] **Step 1: 写失败测试**

Create `internal/cli/root_test.go`:

```go
package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
)

func TestExecuteHelpListsRootCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "skope") {
		t.Errorf("help output does not mention skope:\n%s", stdout.String())
	}
}

func TestExecuteUnknownCommandReturnsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"no-such-command"}, &stdout, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no-such-command") {
		t.Errorf("stderr should name the unknown command, got %q", stderr.String())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/cli/ 2>&1 | head -5
```
Expected: 编译错误，含 `no required module provides package github.com/scarb/skope/internal/cli` 或 `undefined: cli.Execute`。

- [ ] **Step 3: 添加 cobra 依赖**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go get github.com/spf13/cobra@v1.10.2
```
Expected: `go: added github.com/spf13/cobra v1.10.2` 及其间接依赖（`spf13/pflag`、`inconshreveable/mousetrap`）。

- [ ] **Step 4: 实现根命令**

Create `internal/cli/root.go`:

```go
// Package cli defines the skope command tree. It contains no business
// logic; every command delegates to an internal package.
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// Execute runs skope with the given arguments and returns the process
// exit code. It never calls os.Exit so tests can drive it directly.
func Execute(args []string, stdout, stderr io.Writer, version string) int {
	root := newRootCmd(version)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "skope",
		Short:         "Launch coding agents with a session-scoped skill whitelist",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newVersionCmd(version))
	return root
}
```

注意：`newVersionCmd` 在 Task 4 实现。为了让本 Task 独立编译通过，先创建一个最小占位文件 `internal/cli/version.go`：

```go
package cli

import "github.com/spf13/cobra"

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:    "version",
		Hidden: true,
	}
}
```

Task 4 会用完整实现替换它。

- [ ] **Step 5: 运行测试确认通过**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/cli/ -v 2>&1 | tail -6
```
Expected:
```
--- PASS: TestExecuteHelpListsRootCommand
--- PASS: TestExecuteUnknownCommandReturnsOne
PASS
ok  	github.com/scarb/skope/internal/cli
```

- [ ] **Step 6: 创建 main 入口**

Create `cmd/skope/main.go`:

```go
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
```

- [ ] **Step 7: 构建并手工验证**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go build -o skope.exe ./cmd/skope && ./skope.exe --help && rm skope.exe
```
Expected: 打印 usage，含 `skope` 与 `Available Commands`。

- [ ] **Step 8: 格式化、vet、整理依赖**

Run:
```bash
cd D:/workspace/vibe/skill-scope && gofmt -l . && go vet ./... && go mod tidy && git status --short
```
Expected: `gofmt -l` 无输出；`go vet` 无输出；`git status` 列出 `go.mod`、`go.sum`、`cmd/`、`internal/`。

- [ ] **Step 9: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add go.mod go.sum cmd internal && git commit -m "feat: add cobra root command and Execute seam"
```

---

### Task 4: `version` 子命令

**Files:**
- Modify: `internal/cli/version.go`（替换 Task 3 的占位）
- Create: `internal/cli/version_test.go`

- [ ] **Step 1: 写失败测试**

Create `internal/cli/version_test.go`:

```go
package cli_test

import (
	"bytes"
	"testing"

	"github.com/scarb/skope/internal/cli"
)

func TestVersionPrintsInjectedVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"version"}, &stdout, &stderr, "1.2.3")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "skope 1.2.3\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestVersionAppearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	cli.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if !bytes.Contains(stdout.Bytes(), []byte("version")) {
		t.Errorf("help should list the version command:\n%s", stdout.String())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/cli/ -run 'TestVersion' 2>&1 | tail -8
```
Expected: 两个测试 FAIL。`TestVersionPrintsInjectedVersion` 输出 `stdout = "", want "skope 1.2.3\n"`；`TestVersionAppearsInHelp` 因 `Hidden: true` 找不到 `version`。

- [ ] **Step 3: 实现**

Replace `internal/cli/version.go` with:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the skope version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "skope %s\n", version)
			return err
		},
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/cli/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'
```
Expected: 四个 `--- PASS` 与一行 `ok`。

- [ ] **Step 5: 手工验证 ldflags 注入**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go build -ldflags "-X main.version=0.0.1-test" -o skope.exe ./cmd/skope && ./skope.exe version && rm skope.exe
```
Expected: `skope 0.0.1-test`

- [ ] **Step 6: 提交**

```bash
cd D:/workspace/vibe/skill-scope && gofmt -l . && go vet ./... && git add internal/cli && git commit -m "feat: add version command"
```

---

### Task 5: fake agent 可执行程序

**Files:**
- Create: `internal/testutil/fakeagent/main.go`

fake agent 是 spec §12 集成测试的替身。Phase 1 起，集成测试把 `config.toml` 的 `command` 指向它，然后读它写出的 JSON 断言 argv、env、cwd。它是独立 main 包，没有单元测试；Task 6 的 `BuildFakeAgent` 测试会端到端覆盖它。

协议：

| 环境变量 | 含义 |
|---|---|
| `FAKEAGENT_OUT` | 必填。JSON 输出文件的绝对路径。缺失时 fake agent 向 stderr 报错并以退出码 99 退出 |
| `FAKEAGENT_EXIT` | 可选。退出码，默认 0。非整数时以退出码 98 退出 |

输出 JSON：

```json
{"args":["--settings","/x/settings.json"],"env":{"HOME":"/home/u"},"cwd":"/work"}
```

`args` 不含 argv[0]。`env` 是完整环境的 map。

- [ ] **Step 1: 实现**

Create `internal/testutil/fakeagent/main.go`:

```go
// Command fakeagent stands in for claude/codex/opencode in integration
// tests. It records its argv, environment and working directory as JSON
// to the file named by FAKEAGENT_OUT, then exits with FAKEAGENT_EXIT
// (default 0).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	exitMissingOut = 99
	exitBadExit    = 98
	exitWriteFail  = 97
)

type record struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Cwd  string            `json:"cwd"`
}

func main() {
	os.Exit(run())
}

func run() int {
	out := os.Getenv("FAKEAGENT_OUT")
	if out == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: FAKEAGENT_OUT is not set")
		return exitMissingOut
	}

	exitCode := 0
	if raw := os.Getenv("FAKEAGENT_EXIT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fakeagent: FAKEAGENT_EXIT=%q is not an integer\n", raw)
			return exitBadExit
		}
		exitCode = parsed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: getwd: %v\n", err)
		return exitWriteFail
	}

	rec := record{
		Args: os.Args[1:],
		Env:  environMap(os.Environ()),
		Cwd:  cwd,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: marshal: %v\n", err)
		return exitWriteFail
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write %s: %v\n", out, err)
		return exitWriteFail
	}
	return exitCode
}

// environMap converts KEY=VALUE pairs into a map. Entries without '='
// are skipped; on Windows the leading "=C:=..." drive entries are
// skipped as well because their key is empty.
func environMap(pairs []string) map[string]string {
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}
	return env
}
```

- [ ] **Step 2: 构建并手工验证协议**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go build -o fakeagent.exe ./internal/testutil/fakeagent && FAKEAGENT_OUT="$PWD/fa.json" FAKEAGENT_EXIT=7 ./fakeagent.exe --settings /x/s.json -- extra; echo "exit=$?"; cat fa.json; echo; rm fakeagent.exe fa.json
```
Expected: `exit=7`，JSON 中 `"args":["--settings","/x/s.json","--","extra"]`，`"cwd"` 为当前目录，`"env"` 含 `FAKEAGENT_OUT`。

- [ ] **Step 3: 验证缺失 `FAKEAGENT_OUT` 的错误路径**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go build -o fakeagent.exe ./internal/testutil/fakeagent && ./fakeagent.exe; echo "exit=$?"; rm fakeagent.exe
```
Expected: stderr `fakeagent: FAKEAGENT_OUT is not set`，`exit=99`。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && gofmt -l . && go vet ./... && git add internal/testutil && git commit -m "test: add fakeagent binary for integration tests"
```

---

### Task 6: `BuildFakeAgent` 编译辅助

**Files:**
- Create: `internal/testutil/build.go`
- Create: `internal/testutil/build_test.go`

`BuildFakeAgent` 在测试进程内用 `go build` 把 fakeagent 编译到临时目录，返回可执行路径。Phase 1 的 `internal/cli` 集成测试在 `TestMain` 中调用一次并复用。它依赖 `go` 命令在 PATH 上，这在 `go test` 环境下总是成立。

- [ ] **Step 1: 写失败测试**

Create `internal/testutil/build_test.go`:

```go
package testutil_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scarb/skope/internal/testutil"
)

func TestBuildFakeAgentProducesRunnableBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}

	bin := testutil.BuildFakeAgent(t)

	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binary not found at %s: %v", bin, err)
	}

	outFile := filepath.Join(t.TempDir(), "out.json")
	cmd := exec.Command(bin, "--flag", "value")
	cmd.Env = append(os.Environ(),
		"FAKEAGENT_OUT="+outFile,
		"FAKEAGENT_EXIT=3",
	)
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errorsAs(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("expected exit code 3, got err=%v", err)
	}

	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rec struct {
		Args []string          `json:"args"`
		Env  map[string]string `json:"env"`
		Cwd  string            `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("unmarshal record: %v\n%s", err, raw)
	}
	if len(rec.Args) != 2 || rec.Args[0] != "--flag" || rec.Args[1] != "value" {
		t.Errorf("args = %v, want [--flag value]", rec.Args)
	}
	if rec.Env["FAKEAGENT_EXIT"] != "3" {
		t.Errorf("env FAKEAGENT_EXIT = %q, want 3", rec.Env["FAKEAGENT_EXIT"])
	}
	if rec.Cwd == "" {
		t.Error("cwd is empty")
	}
}

func TestBuildFakeAgentReturnsSamePathWithinOneTest(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}

	first := testutil.BuildFakeAgent(t)
	second := testutil.BuildFakeAgent(t)

	if first != second {
		t.Errorf("expected cached path, got %q then %q", first, second)
	}
}
```

在同一文件底部加上 `errorsAs` 辅助，避免为一个调用引入包级 import 混淆：

```go
func errorsAs(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	e, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	*target = e
	return true
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/testutil/ 2>&1 | head -3
```
Expected: `undefined: testutil.BuildFakeAgent`。

- [ ] **Step 3: 实现**

Create `internal/testutil/build.go`:

```go
// Package testutil holds helpers shared by skope's test suites. It is
// imported only from _test.go files.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

const fakeAgentPkg = "github.com/scarb/skope/internal/testutil/fakeagent"

var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
)

// BuildFakeAgent compiles internal/testutil/fakeagent into a temporary
// directory and returns the executable path. The build runs at most
// once per test binary. Because tests share it, the directory remains in
// os.TempDir for the host's normal temporary-file cleanup.
func BuildFakeAgent(tb testing.TB) string {
	tb.Helper()

	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "skope-fakeagent-")
		if err != nil {
			buildErr = err
			return
		}
		name := "fakeagent"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		out := filepath.Join(dir, name)

		cmd := exec.Command("go", "build", "-o", out, fakeAgentPkg)
		cmd.Env = os.Environ()
		if output, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{output: string(output), err: err}
			return
		}
		buildPath = out
	})

	if buildErr != nil {
		tb.Fatalf("build fakeagent: %v", buildErr)
	}
	return buildPath
}

type buildError struct {
	output string
	err    error
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

func (e *buildError) Unwrap() error { return e.err }
```

说明：编译产物放在 `os.MkdirTemp` 而不是 `tb.TempDir()`，因为 `sync.Once` 让多个测试共享同一路径，`tb.TempDir()` 会在第一个测试结束时删掉它。目录留在 `os.TempDir()`，交给主机的例行临时文件清理。

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/testutil/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'
```
Expected: 两个 `--- PASS` 与一行 `ok`。第一次运行含编译耗时（数秒）。

- [ ] **Step 5: 确认 `-short` 跳过**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go test ./internal/testutil/ -short -v 2>&1 | grep -c SKIP
```
Expected: `2`

- [ ] **Step 6: 提交**

```bash
cd D:/workspace/vibe/skill-scope && gofmt -l . && go vet ./... && git add internal/testutil && git commit -m "test: add BuildFakeAgent helper"
```

---

### Task 7: golangci-lint v2 配置与 depguard 依赖规则

**Files:**
- Create: `.golangci.yml`

spec §11.1 要求用 depguard 固化三条禁令：任何包不得 import `cli` 或 `launch`（`cli` 自身除外，`cmd/skope` 除外）；`agent/<name>` 之间不得互相 import；叶子包不得 import `agent`。Phase 0 只有 `cli`、`testutil`，规则现在写全，后续 Phase 加包时无需改配置。

- [ ] **Step 1: 写配置**

Create `.golangci.yml`:

```yaml
version: "2"

run:
  timeout: 5m
  tests: true

linters:
  default: standard
  enable:
    - depguard
    - errorlint
    - gocritic
    - misspell
    - revive
    - unparam
  settings:
    depguard:
      rules:
        # No package may depend on the CLI layer or the launch use-case layer.
        no-upward-imports:
          list-mode: lax
          files:
            - "!**/cmd/skope/**"
            - "!**/internal/cli/**"
            - "!**/internal/launch/**"
          deny:
            - pkg: "github.com/scarb/skope/internal/cli"
              desc: "only cmd/skope may import internal/cli (spec §11.1)"
            - pkg: "github.com/scarb/skope/internal/launch"
              desc: "only internal/cli may import internal/launch (spec §11.1)"
        # Adapter implementations must not import each other.
        no-cross-adapter-claude:
          list-mode: lax
          files:
            - "**/internal/agent/claude/**"
          deny:
            - pkg: "github.com/scarb/skope/internal/agent/codex"
              desc: "adapters must not import each other (spec §11.1)"
            - pkg: "github.com/scarb/skope/internal/agent/opencode"
              desc: "adapters must not import each other (spec §11.1)"
        no-cross-adapter-codex:
          list-mode: lax
          files:
            - "**/internal/agent/codex/**"
          deny:
            - pkg: "github.com/scarb/skope/internal/agent/claude"
              desc: "adapters must not import each other (spec §11.1)"
            - pkg: "github.com/scarb/skope/internal/agent/opencode"
              desc: "adapters must not import each other (spec §11.1)"
        no-cross-adapter-opencode:
          list-mode: lax
          files:
            - "**/internal/agent/opencode/**"
          deny:
            - pkg: "github.com/scarb/skope/internal/agent/claude"
              desc: "adapters must not import each other (spec §11.1)"
            - pkg: "github.com/scarb/skope/internal/agent/codex"
              desc: "adapters must not import each other (spec §11.1)"
        # Leaf packages must not import the agent abstraction.
        leaves-no-agent:
          list-mode: lax
          files:
            - "**/internal/session/**"
            - "**/internal/projection/**"
            - "**/internal/handoff/**"
            - "**/internal/proc/**"
            - "**/internal/termsafe/**"
            - "**/internal/host/**"
          deny:
            - pkg: "github.com/scarb/skope/internal/agent"
              desc: "leaf packages must not import internal/agent (spec §11.1)"
    revive:
      rules:
        - name: exported
          disabled: true
  exclusions:
    generated: lax
    presets:
      - common-false-positives
      - std-error-handling

formatters:
  enable:
    - gofmt
    - goimports
```

- [ ] **Step 2: 运行 lint 确认配置合法且当前代码干净**

golangci-lint 本机未安装，用 `go run` 拉取固定版本。首次运行会下载并编译，约 1 到 3 分钟。

Run:
```bash
cd D:/workspace/vibe/skill-scope && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./... && echo LINT_OK
```
Expected: `LINT_OK`，无 issue。若报配置错误（如某 linter 名不存在），按错误信息修正名字后重跑。

- [ ] **Step 3: 验证 depguard 规则确实生效**

临时创建一个违规文件，确认被拦截，然后删除：

```bash
cd D:/workspace/vibe/skill-scope && mkdir -p internal/host && cat > internal/host/violate.go <<'EOF'
package host

import _ "github.com/scarb/skope/internal/cli"
EOF
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./... 2>&1 | grep -E 'depguard|spec' ; rm -rf internal/host
```
Expected: 输出含 `only cmd/skope may import internal/cli (spec §11.1) (depguard)`。之后 `internal/host` 目录被删除，`git status` 不应有残留。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git status --short && git add .golangci.yml && git commit -m "chore: add golangci-lint v2 config with depguard layering rules"
```

---

### Task 8: GitHub Actions CI

**Files:**
- Create: `.github/workflows/ci.yml`

三平台矩阵。`-race` 只在 ubuntu 与 macos 跑；windows runner 虽有 gcc，但为了与本机行为一致且避免 cgo 链路的不确定性，windows job 不带 `-race`。lint 只在 ubuntu 跑一次。

- [ ] **Step 1: 写 workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  test:
    name: test (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
          check-latest: true
      - name: go vet
        run: go vet ./...
      - name: go test (race)
        if: runner.os != 'Windows'
        run: go test -race -coverprofile=coverage.txt ./...
      - name: go test
        if: runner.os == 'Windows'
        run: go test '-coverprofile=coverage.txt' ./...
      - name: build
        run: go build ./...

  lint:
    name: lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
          check-latest: true
      - name: gofmt
        run: |
          out="$(gofmt -l .)"
          if [ -n "$out" ]; then
            echo "gofmt needed on:"; echo "$out"; exit 1
          fi
      - uses: golangci/golangci-lint-action@v9
        with:
          version: v2.13.2
```

- [ ] **Step 2: 本地校验 YAML 语法**

Run:
```bash
cd D:/workspace/vibe/skill-scope && python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml')); print('YAML_OK')"
```
Expected: `YAML_OK`。若本机无 PyYAML，改用 `go run github.com/goccy/go-yaml/cmd/ycat@latest .github/workflows/ci.yml >/dev/null && echo YAML_OK`。

- [ ] **Step 3: 本地模拟 CI 步骤（不含 -race）**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go vet ./... && go test -coverprofile=coverage.txt ./... && go build ./... && echo CI_STEPS_OK && rm -f coverage.txt
```
Expected: 各包 `ok`，最后 `CI_STEPS_OK`。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add .github && git commit -m "ci: add three-platform test and lint workflow"
```

---

### Task 9: goreleaser 骨架

**Files:**
- Create: `.goreleaser.yaml`

Phase 6 才真正发布，本 Task 只放一份能通过 `goreleaser check` 的最小配置，锁定 ldflags 版本注入方式与产物命名。Homebrew tap 与 Scoop bucket 的仓库名在 Phase 6 确定后再补。

- [ ] **Step 1: 写配置**

Create `.goreleaser.yaml`:

```yaml
version: 2

project_name: skope

before:
  hooks:
    - go mod tidy

builds:
  - id: skope
    main: ./cmd/skope
    binary: skope
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{ .Version }}

archives:
  - id: default
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}

checksum:
  name_template: checksums.txt

changelog:
  use: github
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"

release:
  draft: true
```

- [ ] **Step 2: 校验配置**

前置：`goreleaser check` 需要 `origin` 是可识别的 GitHub、GitLab 或 Gitea URL。仓库尚未配置真实 remote 时，在一次性 clone 中设置预期 SCM URL 后验证，不要给工作仓库写入猜测的 remote。

Run:
```bash
cd D:/workspace/vibe/skill-scope && go run github.com/goreleaser/goreleaser/v2@v2.18.0 check
```
Expected: 输出含 `1 configuration file(s) validated`。若报字段名错误（goreleaser 版本间偶有字段改名），按提示修正。

- [ ] **Step 3: 本地快照构建验证 ldflags**

Run:
```bash
cd D:/workspace/vibe/skill-scope && go run github.com/goreleaser/goreleaser/v2@v2.18.0 build --snapshot --clean --single-target && ./dist/skope_windows_amd64_v1/skope.exe version; rm -rf dist
```
Expected: `skope 0.0.0-SNAPSHOT-<hash>` 或类似快照版本字符串（非 `dev`）。目录名以实际输出为准，若不同用 `find dist -name 'skope*'` 定位。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git status --short && git add .goreleaser.yaml && git commit -m "chore: add goreleaser skeleton with ldflags version injection"
```

---

### Task 10: Makefile 开发入口

**Files:**
- Create: `Makefile`

把每期都要跑的校验固化为一个命令，避免各 Phase 的 plan 重复写长命令。Git Bash 下 `make` 可用；若本机没有 make，每个 target 的命令都可以直接复制执行。

- [ ] **Step 1: 写 Makefile**

Create `Makefile`:

```makefile
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
GORELEASER    := go run github.com/goreleaser/goreleaser/v2@v2.18.0

.PHONY: all fmt vet lint test test-race build check release-check clean

all: check

fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...

test:
	go test ./...

# Requires cgo (a C compiler). Not available on the Windows dev box; CI runs it on ubuntu/macos.
test-race:
	go test -race ./...

build:
	go build -o skope$(EXE) ./cmd/skope

# Everything that must be green before a phase is considered complete (spec §14.7).
check: fmt vet lint test build

release-check:
	$(GORELEASER) check

clean:
	rm -f skope skope.exe coverage.txt
	rm -rf dist

ifeq ($(OS),Windows_NT)
EXE := .exe
endif
```

- [ ] **Step 2: 运行 `make check`**

Run:
```bash
cd D:/workspace/vibe/skill-scope && (make --version >/dev/null 2>&1 && make check && echo MAKE_OK) || echo "make not installed; run the targets' commands manually"
```
Expected: `MAKE_OK`，中间各步无错误。若 make 未安装，手工依次运行 fmt/vet/lint/test/build 对应命令并确认全部成功。

- [ ] **Step 3: 确认 build 产物被 .gitignore 忽略**

Run:
```bash
cd D:/workspace/vibe/skill-scope && git status --short
```
Expected: 只有 `?? Makefile`，没有 `skope.exe`。

- [ ] **Step 4: 提交**

```bash
cd D:/workspace/vibe/skill-scope && make clean 2>/dev/null; git add Makefile && git commit -m "chore: add Makefile with check target"
```

---

### Task 11: README

**Files:**
- Create: `README.md`

面向贡献者的最小 README：项目一句话定位、状态、开发命令、spec 与 plan 的位置。用户文档等 Phase 5 之后再写。

- [ ] **Step 1: 写 README**

Create `README.md`:

```markdown
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
```

- [ ] **Step 2: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add README.md && git commit -m "docs: add contributor README"
```

---

### Task 12: `docs/verification.md` 模板

**Files:**
- Create: `docs/verification.md`

spec §7.5 与 §14.7 要求所有真实 agent 验证结论累积在这一个文件里。Phase 0 只建模板：按 agent 分组，每条预留固定字段，状态全部为「未验证」。各 Phase 执行时填写。

- [ ] **Step 1: 写模板**

Create `docs/verification.md`:

```markdown
# 真实 agent 验证记录

对应 spec §7.5。每条记录 agent 版本、完整命令、可重复的 fixture 目录布局和结论。
结论与 spec §3/§4.1/§7 不符时，先修订 spec 再开始对应 adapter 实现。

约束：绝不修改用户真实的 `~/.claude`、`~/.codex`、`~/.config/opencode`。
实验全部通过 `CLAUDE_CONFIG_DIR`、`CODEX_HOME`、`OPENCODE_CONFIG_DIR`、
`OPENCODE_CONFIG_CONTENT` 重定向到临时 fixture 目录。

状态取值：`未验证` / `通过` / `不符（已修订 spec §x）`。

## Claude（阻断 Phase 2）

### 第 3 条：`--add-dir` 投影 skill 可被 `skillOverrides` 打开

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 10 条：plugin 缓存目录中 skill 的枚举来源与布局

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

## Codex（阻断 Phase 3）

### 第 1 条：`-c skills.config` 按 `path` 关闭 skill

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 6 条：项目级 skills 目录的扫描范围

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 9 条：plugin ID 格式与 `skills.config` 跨层合并语义

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

## OpenCode（阻断 Phase 4）

### 第 2 条：`skills.paths` 注入、`permission.skill` 优先级、内置 skill 清单

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：
- 内置 skill 清单：

### 第 4 条：`OPENCODE_CONFIG_CONTENT` 数组字段合并语义

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 5 条：项目级具体 allow 与 `OPENCODE_CONFIG_CONTENT` 层 `*` deny 谁胜出

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 7 条：兼容目录中嵌套项目 skill 的有效名

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 8 条：配置文件发现与合并顺序、jsonc 支持

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

## 各期手工验收

按 Phase 追加。每条记录日期、agent 版本、命令、观察。

### Phase 1

（待填写）
```

- [ ] **Step 2: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add docs/verification.md && git commit -m "docs: add verification log template for spec §7.5"
```

---

### Task 13: 回写 spec（Phase 0 定义与 `-race` 平台说明）

**Files:**
- Modify: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`（§14 分期表与 §14.7）

两处事实需要回写：spec §14 没有 Phase 0，本 plan 定义了它；spec §14.7 要求每期 `go test -race` 全绿，但 Windows 开发机没有 cgo。

- [ ] **Step 1: 在 §14 分期表前加 Phase 0 行**

找到 §14 中的表格：

```markdown
| Phase | 交付 | 前置验证（§7.5） | 依赖 |
|---|---|---|---|
| 1 | Claude 最小闭环（Unix） | 无 | 无 |
```

改为：

```markdown
| Phase | 交付 | 前置验证（§7.5） | 依赖 |
|---|---|---|---|
| 0 | 项目准备：模块、cobra 根命令与 `version`、fakeagent 与 `BuildFakeAgent`、golangci-lint v2 与 depguard 分层规则、三平台 CI、goreleaser 骨架、`docs/verification.md` 模板 | 无 | 无 |
| 1 | Claude 最小闭环（Unix） | 无 | Phase 0 |
```

- [ ] **Step 2: 修改 §14.7 第一条**

找到：

```markdown
- 每期内按 TDD 推进，`gofmt`、`go vet`、`go test -race ./...` 全绿才进入下一期。
```

改为：

```markdown
- 每期内按 TDD 推进，`make check`（`gofmt`、`go vet`、`golangci-lint`、`go test ./...`、`go build`）本地全绿，且 CI 三平台通过（ubuntu/macos 带 `-race`，Windows 不带，因 `-race` 依赖 cgo）才进入下一期。
```

- [ ] **Step 3: 在 §14.1 Phase 1 范围中去掉已由 Phase 0 完成的项**

找到 §14.1 范围列表最后一项：

```markdown
- `internal/testutil/fakeagent` 与 `internal/cli` 集成测试骨架（§12），本期起每期沿用。
```

改为：

```markdown
- `internal/cli` 集成测试（§12），使用 Phase 0 提供的 `testutil.BuildFakeAgent`，本期起每期沿用。
```

同一节中的：

```markdown
- `internal/cli`：`skope claude` 的 `-s`/`--set`（含 `none`、重复报错、逗号并集）、`--dry-run`、`--` 透传切分、非 TTY 未传 `-s` 报错、§6.1 argv 顺序；`list`；`version`。
```

改为（`version` 已在 Phase 0 完成）：

```markdown
- `internal/cli`：`skope claude` 的 `-s`/`--set`（含 `none`、重复报错、逗号并集）、`--dry-run`、`--` 透传切分、非 TTY 未传 `-s` 报错、§6.1 argv 顺序；`list`。
```

- [ ] **Step 4: 确认无其他残留引用**

Run:
```bash
cd D:/workspace/vibe/skill-scope && grep -n 'go test -race' docs/superpowers/specs/2026-09-02-skill-scope-design.md
```
Expected: 只剩 §2 技术栈表与 §12、§14.6 中「CI 运行」语境的引用，不再有「每期本地必须跑 -race」的表述。

- [ ] **Step 5: 提交**

```bash
cd D:/workspace/vibe/skill-scope && git add docs/superpowers/specs/2026-09-02-skill-scope-design.md && git commit -m "docs: define Phase 0 in spec and relax -race to CI-only"
```

---

### Task 14: Phase 0 收尾验证

**Files:** Modify: 本 plan、`docs/verification.md`。

- [x] **Step 1: 干净 clone 验证**

从零 clone 到临时目录，确认没有依赖本地未提交状态：

Run: `git clone` 到一次性目录后依次执行 `go vet ./...`、`go test ./...`、`go build -o skope.exe ./cmd/skope`、`./skope.exe version`。
Expected: 各包 `ok`，最后一行 `skope dev`。

- [x] **Step 2: 确认最终文件树与本 plan「文件结构」一致**

Run: `cd D:/workspace/vibe/skill-scope && git ls-files`
Expected: 19 个 tracked 文件，与本 plan「文件结构」及 Task 1-13 的 Files 清单一致。

- [x] **Step 3: 确认提交历史**

Run: `cd D:/workspace/vibe/skill-scope && git log --format='%s'`
Expected: 所有提交主题符合 Conventional Commits。修复与验证提交会改变总数，因此不锁定固定数量。

收尾验证已完成，证据记入 `docs/verification.md`。Phase 0 满足 spec §14.7，可以进入 Phase 1。

---

## Self-Review

**Spec coverage（仅限 Phase 0 范围，即 §11、§12 中与业务无关的基础设施）：**

| spec 要求 | Task |
|---|---|
| §11 `cmd/skope/main.go` 唯一 main，ldflags 注入版本 | 3、4、9 |
| §11 `internal/cli/root.go` 与 `Execute` seam | 3 |
| §11 `internal/testutil/fakeagent/main.go` 记录 argv/env/cwd 后按指定码退出 | 5 |
| §11 `internal/testutil/build.go` 编译 fakeagent 到临时目录 | 6 |
| §11.1 depguard 固化依赖方向 | 7 |
| §11.2 黑盒测试 `package xxx_test` | 3、4、6 均用 `_test` 包 |
| §11.2 `testing.Short()` 跳过构建型测试 | 6 |
| §12 CI 运行 `go test -race`、`go vet`、golangci-lint | 8 |
| §2 goreleaser | 9 |
| §7.5 / §14.7 `docs/verification.md` 单一累积文件 | 12 |
| §14.7 每期校验命令 | 10、13 |

未覆盖且有意排除：`completion` 子命令（cobra 默认自动提供，无需代码）；`.golangci.yml` 中 §11.2「文件 800 行上限」未用 linter 固化（`funlen`/`lll` 不直接测文件行数，留人工 review）。

**Placeholder scan：** 全文无 TBD/TODO；Task 12 的模板里「（待填写）」与空字段是文档本身的设计内容，不是 plan 占位。Task 9 Step 3 的 dist 目录名给出了备用定位命令。

**Type consistency：** `cli.Execute(args []string, stdout, stderr io.Writer, version string) int` 在 Task 3 定义，Task 3/4 测试与 `cmd/skope/main.go` 调用签名一致。`testutil.BuildFakeAgent(tb testing.TB) string` 在 Task 6 定义，其测试调用一致。fakeagent 的环境变量名 `FAKEAGENT_OUT`/`FAKEAGENT_EXIT` 与退出码 99/98/97 在 Task 5 定义、Task 6 测试中使用一致。
