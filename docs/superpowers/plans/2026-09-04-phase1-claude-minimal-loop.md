# skill-scope Phase 1（Claude Unix 最小闭环）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 Linux/macOS 上完成 `skope claude -s <set>` 的第一条真实闭环：读取配置、扫描 Claude 原生 skill/command、生成逐项 `skillOverrides`、创建会话目录，并以 `syscall.Exec` 把当前进程替换为 Claude。

**Architecture:** 先实现无副作用的 `host`、`config`、`skill` 数据与解析层，再冻结 `agent` 接口和 Claude adapter，最后由 `launch` 按 spec §8.1 编排配置、扫描、解析、会话、规划与 handoff。CLI 只负责 Cobra 命令、参数切分和文本渲染；业务包通过显式依赖注入测试。Phase 1 只启用 Claude 原生入口，因此解析结果只有 `native` 和 `missing`，但冻结类型会预留后续 `projected`、`unavailable`、plugin 和 bundled 所需字段。

**Tech Stack:** Go 1.24+、spf13/cobra v1.10.2、pelletier/go-toml/v2 v2.4.3、gopkg.in/yaml.v3 v3.0.1、golang.org/x/sys v0.41.0（仅 Darwin 进程启动时间）、标准库 `testing` / `testing/fstest` / `syscall`。

**Spec 对应：** `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §4、§5.1–§5.3、§5.5、§6.1、§6.3 的 Phase 1 子集、§6.4 `list`、§7/§7.1 的 `skillOverrides` 子集、§8.1/§8.2/§8.4 Unix、§10、§11、§12、§14.1/§14.7。

**执行技能：** 每个 Task 按 `@superpowers:test-driven-development` 的红—绿—重构顺序执行；Task 16 按 `@superpowers:verification-before-completion` 收尾。不要提前使用 Phase 2 的 projection、proc、termsafe 或 plugin 实现。

**执行状态：** 未开始。

---

## 前置事实与已定决策

- **起点：** `main` 当前 HEAD 为 `bf6c4ed`，Phase 0 已关闭；当前未跟踪项是用户的 `AGENTS.md` 与本计划文件。提交本计划时只 stage 本文件，`AGENTS.md` 不得修改或提交。
- **执行工作区：** 本计划文档进入版本控制后，执行者必须先使用 `@superpowers:using-git-worktrees` 从最新 `main` 创建 `codex/phase1-claude-minimal-loop` 独立 worktree，再开始 Task 1；不得直接在当前 `main` 工作区实施。`AGENTS.md` 继续留在原工作区，不复制、不提交。
- **计划位置：** Repository Guidelines 要求 phase plan 放在 `docs/superpowers/plans/`，优先于 writing-plans Skill 的通用 `docs/plans/` 默认值。
- **CodeGraph：** 仓库根没有 `.codegraph/`，执行时直接使用 `rg` 与精确文件读取。
- **TOML 顺序：** `skillsets.toml` 用 `toml.Decoder.DisallowUnknownFields()` 严格解码；配置顺序用同版本的 `unstable.Parser` 按 `[skillsets.<name>]` 第一次出现顺序提取。选择该方案，因为普通 Go map 不保序，而自写正则无法正确处理 TOML 引号键。
- **文件系统：** `config`、`skill` 不 import `os`。它们定义最小只读接口；`host.OSFileSystem` 是生产适配器。`fstest.MapFS` 通过测试包装器驱动扫描逻辑。
- **Phase 1 平台边界：** Linux/macOS 使用 `syscall.Exec`。Windows 构建提供明确的 `handoff.ErrUnsupported`，相关端到端测试跳过；不得用临时 `os/exec` 行为冒充 Phase 6 的 Job Object 语义。
- **未传 `-s`：** Phase 1 尚无 TTY 选择器，统一返回“请显式传 `-s <name>` 或 `-s none`”。这覆盖本期要求的非 TTY 错误；Phase 5 再增加 TTY 分支。
- **`-s none`：** 仍读取并校验 `config.toml`、回收旧会话、解析 agent command；不读取 `skillsets.toml`、不扫描 skill、不创建新会话、不生成 `--settings`。
- **dry-run：** 执行配置、扫描、解析与 Claude planning；会回收旧会话，但只创建内存中的 preview session，不创建目录、不 handoff。
- **settings 范围：** Phase 1 只生成 `skillOverrides`。即使 `skillsets.toml` 已完整校验 `plugins` 与 `bundled`，adapter 也不生成 `enabledPlugins` 或 `disableBundledSkills`。
- **类型冻结：** Task 4 与 Task 7 的六个类型在 Phase 1 结束后冻结：`skill.Skill`、`skill.Location`、`agent.Adapter`、`agent.Capabilities`、`agent.LaunchPlan`、`agent.Inventory`。Task 16 必须逐项复核 spec §4.2/§7。

---

## 目标文件结构

Phase 1 结束时新增或修改以下文件；未列出的 Phase 2+ 目录不要创建。

```text
cmd/skope/main.go                         # 保持 Phase 0 薄入口，原则上无需修改
internal/
├── host/
│   ├── env.go                           # 环境/home/cwd 快照与不可变访问
│   ├── env_test.go
│   └── fs.go                            # 只读 OS 文件系统适配器
├── config/
│   ├── errors.go                        # 带文件和字段路径的错误
│   ├── home.go                          # SKOPE_HOME
│   ├── home_test.go
│   ├── config.go                        # config.toml 只读加载
│   ├── config_test.go
│   ├── skillsets.go                     # skillsets.toml、顺序、并集
│   ├── skillsets_test.go
│   └── validate.go                      # 名字、skill、plugin 校验
├── skill/
│   ├── skill.go                         # 冻结类型与枚举
│   ├── scope.go                         # Claude 扫描根数据
│   ├── scan.go                          # skill/command 遍历
│   ├── scan_test.go
│   ├── identity.go                      # 合并、排序、碰撞
│   ├── identity_test.go
│   ├── resolve.go                       # native/missing
│   └── resolve_test.go
├── agent/
│   ├── agent.go                         # 冻结接口与数据类型
│   ├── registry.go                      # 显式 adapter 注册表，无 init
│   └── claude/
│       ├── adapter.go
│       ├── inventory.go                 # 扫描与三层 settings 键
│       ├── inventory_test.go
│       ├── settings.go                  # settings.json plan
│       ├── settings_test.go
│       └── testdata/settings.golden.json
├── session/
│   ├── session.go                       # preview/stage/write/publish/abort
│   ├── owner.go
│   ├── reap.go
│   ├── session_test.go
│   ├── process.go                       # ProcessInspector 接口
│   ├── process_linux.go
│   ├── process_darwin.go
│   └── process_windows.go               # Phase 1 明确不支持
├── handoff/
│   ├── handoff.go
│   ├── handoff_unix.go
│   └── handoff_windows.go               # ErrUnsupported
├── launch/
│   ├── launch.go                        # §8.1 编排
│   ├── launch_test.go
│   └── render.go                        # Phase 1 最小 summary/dry-run 数据
└── cli/
    ├── root.go
    ├── root_test.go
    ├── args.go
    ├── args_test.go
    ├── launch_cmd.go
    ├── list_cmd.go
    ├── list_cmd_test.go
    └── integration_test.go
```

---

### Task 1: 固定依赖并实现 `host.Env` 与只读 OS 文件系统

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/host/env.go`
- Create: `internal/host/env_test.go`
- Create: `internal/host/fs.go`

`host.Env` 是唯一读取 `os.Environ`、`os.UserHomeDir`、`os.Getwd` 的位置。构造后不暴露内部 map，避免调用者就地修改环境快照。

- [ ] **Step 1: 添加固定版本依赖**

Run:

```bash
cd D:/workspace/vibe/skill-scope
go get github.com/pelletier/go-toml/v2@v2.4.3 gopkg.in/yaml.v3@v3.0.1 golang.org/x/sys@v0.41.0
```

Expected: `go.mod` 出现三个 direct dependency；`golang.org/x/sys` 选择 v0.41.0，因为它的 `go.mod` 仍声明 Go 1.24，不能升级到要求 Go 1.25 的 v0.47.0。

- [ ] **Step 2: 写失败测试**

Create `internal/host/env_test.go`，覆盖以下完整契约：

```go
package host_test

import (
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func TestNewEnvCopiesInputAndReturnsSortedEnviron(t *testing.T) {
	vars := map[string]string{"Z": "last", "A": "first"}
	env := host.NewEnv("/home/me", "/repo", vars)
	vars["A"] = "mutated"

	if got := env.Get("A"); got != "first" {
		t.Fatalf("Get(A) = %q, want first", got)
	}
	if got := env.Environ(); !reflect.DeepEqual(got, []string{"A=first", "Z=last"}) {
		t.Fatalf("Environ() = %#v", got)
	}
	if env.Home() != "/home/me" || env.Cwd() != "/repo" {
		t.Fatalf("Home/Cwd = %q/%q", env.Home(), env.Cwd())
	}
}

func TestWithReturnsCopy(t *testing.T) {
	base := host.NewEnv("/home/me", "/repo", map[string]string{"A": "one"})
	next := base.With(map[string]string{"A": "two", "B": "three"})
	if base.Get("A") != "one" || next.Get("A") != "two" || next.Get("B") != "three" {
		t.Fatalf("base=%v next=%v", base.Environ(), next.Environ())
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/host -run 'Test(NewEnv|With)' -v`

Expected: FAIL，原因是 `internal/host` 或对应标识符尚不存在。

- [ ] **Step 4: 实现最小不可变环境快照**

Create `internal/host/env.go`，对外 API 固定为：

```go
package host

type Env struct { /* home、cwd、vars 均为私有字段 */ }

func Snapshot() (Env, error)
func NewEnv(home, cwd string, vars map[string]string) Env
func (e Env) Home() string
func (e Env) Cwd() string
func (e Env) Get(name string) string
func (e Env) Lookup(name string) (string, bool)
func (e Env) Environ() []string
func (e Env) With(overrides map[string]string) Env
```

实现约束：

- `Snapshot` 依次调用 `os.UserHomeDir`、`os.Getwd`、`os.Environ`；环境条目用 `strings.Cut(pair, "=")`，忽略无等号或空 key 的条目。
- `NewEnv` 和 `With` 都复制 map；`Environ` 返回新 slice，按 key 排序，确保测试和 dry-run 稳定。
- 不做大小写折叠；spec 中使用的环境变量名均为大写，Windows 的额外语义留 Phase 6。

Create `internal/host/fs.go`：

```go
package host

type OSFileSystem struct{}

func (OSFileSystem) ReadFile(name string) ([]byte, error)
func (OSFileSystem) ReadDir(name string) ([]fs.DirEntry, error)
func (OSFileSystem) Stat(name string) (fs.FileInfo, error)
func (OSFileSystem) Lstat(name string) (fs.FileInfo, error)
func (OSFileSystem) EvalSymlinks(name string) (string, error)
```

每个方法只把 slash 路径经 `filepath.FromSlash` 转换后委托给 `os`/`filepath`；`EvalSymlinks` 返回 `filepath.ToSlash` 后的路径。

- [ ] **Step 5: 运行测试与格式化**

Run: `gofmt -w internal/host && go test ./internal/host -v`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add go.mod go.sum internal/host
git commit -m "feat: add immutable host environment snapshot"
```

---

### Task 2: `SKOPE_HOME` 与 `config.toml` 严格只读加载

**Files:**

- Create: `internal/config/errors.go`
- Create: `internal/config/home.go`
- Create: `internal/config/home_test.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: 写 `SKOPE_HOME` 失败测试**

`internal/config/home_test.go` 使用 `t.TempDir()` 生成跨平台绝对路径，覆盖：未设置时为 `<home>/.skope`；trim 后空值回退默认；`~`、`~/x`、`~\x` 展开；绝对 override 接受；相对路径拒绝；`~other/x` 拒绝。

测试使用以下 API：

```go
func ResolveHome(env host.Env) (string, error)
```

Run: `go test ./internal/config -run TestResolveHome -v`

Expected: FAIL，`ResolveHome` 未定义。

- [ ] **Step 2: 实现 home 与路径化错误**

Create `internal/config/errors.go`：

```go
package config

type PathError struct {
	Path  string
	Field string
	Err   error
}

func (e *PathError) Error() string
func (e *PathError) Unwrap() error
```

错误字符串格式固定：字段为空时 `<path>: <err>`；字段非空时 `<path>: <field>: <err>`。后续 CLI 只包装上下文，不重新解析字符串。

Create `internal/config/home.go` 实现 `ResolveHome`。override 先 `strings.TrimSpace`；只展开恰好 `~` 或以 `~/`、`~\` 开头的形式；最后 `filepath.Clean` 并用 `filepath.IsAbs` 校验。读取过程不得创建目录。

- [ ] **Step 3: 写 `config.toml` table test**

`internal/config/config_test.go` 使用实现 `ReadFile(string)` 的内存 FS，至少包含：

| case | input | expected |
|---|---|---|
| missing | `fs.ErrNotExist` | `Exists=false`、空 agents、无错误 |
| valid | version 1、三个合法 agent | command/args 保留 |
| missing version | 只有 agents | 字段 `version` 错误 |
| wrong version | `version=2` | 字段 `version` 错误 |
| unknown root | `extra=true` | 严格解码错误含 `extra` |
| unknown agent | `[agents.other]` | 错误含 `agents.other` |
| unknown agent field | `timeout=1` | 错误含 `agents.claude.timeout` |
| wrong command type | `command=[]` | 解码错误 |
| wrong args member | `args=[1]` | 解码错误 |

API：

```go
type AgentConfig struct {
	Command string
	Args    []string
}

type Config struct {
	Exists bool
	Agents map[string]AgentConfig
}

type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

func Load(fsys ReadFileFS, path string) (Config, error)
```

- [ ] **Step 4: 运行测试确认失败**

Run: `go test ./internal/config -run 'TestLoad' -v`

Expected: FAIL，loader 未定义。

- [ ] **Step 5: 实现严格解码**

Create `internal/config/config.go`。文件结构用私有 DTO，`Version *int` 用于区分缺失与零值：

```go
type configFile struct {
	Version *int       `toml:"version"`
	Agents  agentsFile `toml:"agents"`
}

type agentsFile struct {
	Claude  *agentFile `toml:"claude"`
	Codex   *agentFile `toml:"codex"`
	OpenCode *agentFile `toml:"opencode"`
}

type agentFile struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}
```

实现顺序：

1. `ReadFile`；`errors.Is(err, fs.ErrNotExist)` 返回 `Config{Exists:false, Agents: emptyMap}`。
2. `toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw)`。
3. TOML 错误统一包装为 `PathError{Path:path, Err:err}`；若能 `errors.As` 到 `*toml.DecodeError`，把 `strings.Join(de.Key(), ".")` 放进 `Field`。
4. 校验 version 必填且等于 1。
5. 把三个非 nil agent DTO 映射到固定字符串 key `claude`/`codex`/`opencode`；复制 `Args`。config 不 import skill/agent，launch 用 `string(req.Agent)` 查询。

- [ ] **Step 6: 运行测试并提交**

Run: `gofmt -w internal/config && go test ./internal/config -v`

Expected: PASS。

```bash
git add internal/config
git commit -m "feat: load skope home and agent config"
```

---

### Task 3: `skillsets.toml` 全量校验、配置顺序与多 set 并集

**Files:**

- Create: `internal/config/skillsets.go`
- Create: `internal/config/skillsets_test.go`
- Create: `internal/config/validate.go`

- [ ] **Step 1: 写 loader 与校验的失败测试**

`internal/config/skillsets_test.go` 必须 table-drive spec §5.3 的每条规则：

- version 缺失、非 1；root/set/plugins 未知字段；字段类型错误。
- set 名：空、`none`、以 `-` 开头、含空格、逗号或非法字符失败；`dev`、`foo.bar`、`a_b-c9` 通过。
- `skills` 必填但可为空；元素 trim 后存储；空元素、trim 后重复失败；`apps/web:verify` 通过。
- `plugins` 只接受 `claude`/`codex`；每项必须恰好一个 `@` 且两段非空；trim 后重复失败；`opencode` 被 strict decoder 拒绝。
- `description` 可缺失；`bundled` 缺省归一化为 true，显式 false 保留。
- `[skillsets.second]` 写在 `[skillsets.first]` 前时 `Items` 保持 `second, first`；`[skillsets."foo.bar"]` 的名字正确解码；`skillsets = { # comment\n dev = { skills = [] } }` 不 panic 且顺序正确。
- 文件不存在返回 `Exists=false`，不把它当作损坏。

冻结本包数据 API：

```go
type SkillSet struct {
	Name        string
	Description string
	Skills      []string
	Plugins     map[string][]string
	Bundled     bool
}

type SkillSets struct {
	Exists bool
	Items  []SkillSet
}

type Selection struct {
	DisplayName string
	Skills      []string
	Plugins     map[string][]string
	Bundled     bool
}

func LoadSkillSets(fsys ReadFileFS, path string) (SkillSets, error)
func ParseSelection(value string) ([]string, error)
func (s SkillSets) Merge(names []string) (Selection, error)
func (s SkillSets) Names() []string
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config -run 'Test(LoadSkillSets|ParseSelection|Merge)' -v`

Expected: FAIL。

- [ ] **Step 3: 实现 DTO 与严格校验**

`skillSetFile.Skills` 使用 `*[]string` 区分“缺失”和“空数组”；`Bundled *bool` 区分缺省。`pluginsFile` 只声明 Claude/Codex 两个字段，让 strict decoder 自然拒绝 OpenCode 和其他键。

`validate.go` 提供私有函数：

```go
func validateSetName(name string) error
func normalizeUnique(field string, values []string) ([]string, error)
func validatePluginID(id string) error
```

set 名正则固定为 `^[A-Za-z0-9._-]+$`，另行拒绝前导 `-`、保留名 `none` 和逗号。数组按第一次出现顺序去重检测，发现重复即报错，不静默合并。

- [ ] **Step 4: 用 AST 提取配置顺序**

在 `skillsets.go` 实现：

```go
func skillSetOrder(data []byte) ([]string, error) {
	var parser unstable.Parser
	parser.Reset(data)
	seen := map[string]bool{}
	var order []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	for parser.NextExpression() {
		expr := parser.Expression()
		if expr.Kind != unstable.Table && expr.Kind != unstable.KeyValue {
			continue
		}
		var key []string
		it := expr.Key()
		for it.Next() {
			key = append(key, string(it.Node().Data))
		}
		if len(key) >= 2 && key[0] == "skillsets" {
			add(key[1])
			continue
		}
		if expr.Kind == unstable.KeyValue && len(key) == 1 && key[0] == "skillsets" && expr.Value().Kind == unstable.InlineTable {
			children := expr.Value().Children()
			for children.Next() {
				child := children.Node()
				if child.Kind != unstable.KeyValue {
					continue
				}
				childKey := child.Key()
				if childKey.Next() {
					add(string(childKey.Node().Data))
				}
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	return order, nil
}
```

严格 decoder 负责语义；AST 只负责顺序。上述逻辑同时处理普通 table、dotted key 与 `skillsets = { ... }` inline table。解码 map 中存在但 AST 未捕获的项属于内部不变量错误，必须返回错误，不能按 map 随机顺序补齐。

- [ ] **Step 5: 实现选择表达式和并集**

`ParseSelection`：按逗号切分、trim、拒绝空项；`none` 只能单独出现；普通名字保持输入顺序，重复名字保留第一次。`Merge`：

- 名字不存在时返回类型化 `UnknownSetError{Name, Available}`，可用名按配置顺序。
- skills 和各 agent plugins 都按 set 顺序、成员顺序做稳定并集。
- bundled 用 OR；display name 用 `+` 连接解析后的 set 名。
- `none` 不交给 `Merge`，由 launch 提前分支。

- [ ] **Step 6: 运行测试、全包回归并提交**

Run:

```bash
gofmt -w internal/config
go test ./internal/config -v
go test ./internal/host ./internal/config
```

Expected: PASS。

```bash
git add internal/config
git commit -m "feat: load and merge ordered skill sets"
```

---

### Task 4: 冻结 `Skill` / `Location` 类型并实现身份合并与碰撞

**Files:**

- Create: `internal/skill/skill.go`
- Create: `internal/skill/identity.go`
- Create: `internal/skill/identity_test.go`
- Modify: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`（补齐 admin priority）

- [ ] **Step 1: 先补齐 spec 的 admin priority**

spec §4.3 当前只定义 project/global、source 与 plugin 最低，没有定义 `LevelAdmin`。在开始 comparator 测试前，把 Location 优先级明确为：`project > global > admin > plugin`；理由是 admin `/etc/codex` 低于用户全局配置，但仍是 agent 原生入口，优先于不可跨 agent 投影的 plugin。若 Phase 3 的真实 Codex 验证推翻该顺序，必须先修订 spec 再调整实现。

- [ ] **Step 2: 写冻结类型**

Create `internal/skill/skill.go`，一次性定义完整冻结类型，不提交半成品文件。字段与 spec §4.2 一一对应，不增加展示字符串：

```go
package skill

type Agent string
type Kind string
type Level string
type Source string

const (
	AgentClaude   Agent = "claude"
	AgentCodex    Agent = "codex"
	AgentOpenCode Agent = "opencode"

	KindSkill   Kind = "skill"
	KindCommand Kind = "command"

	LevelGlobal  Level = "global"
	LevelProject Level = "project"
	LevelPlugin  Level = "plugin"
	LevelAdmin   Level = "admin"

	SourceClaude       Source = "claude"
	SourceAgents       Source = "agents"
	SourceCodex        Source = "codex"
	SourceOpenCode     Source = "opencode"
	SourceOpenCodePath Source = "opencode-paths"
)

type Skill struct {
	ID        string
	Locations []Location
}

type Location struct {
	Kind            Kind
	DiscoveryPath   string
	RealPath        string
	Level           Level
	Source          Source
	Scope           string
	FrontmatterName string
	Names           map[Agent]string
	PluginID        string
	PluginAgent     Agent
}
```

同文件定义结构化碰撞，不把用户文本提前拼成 message：

```go
type CollisionKind string

const (
	CollisionDifferentContent CollisionKind = "different-content"
	CollisionFrontmatterName  CollisionKind = "frontmatter-name"
	CollisionEffectiveName    CollisionKind = "effective-name"
)

type Collision struct {
	Kind  CollisionKind
	IDs   []string
	Agent Agent
	Name  string
	Paths []string
}
```

- [ ] **Step 3: 写身份测试**

`identity_test.go` 覆盖：

1. 同 ID 的 skill 与 command 合并，两个 location 都保留。
2. 去重键只有 `(Source, DiscoveryPath)`；RealPath 相同但 discovery/source 不同不得去重。
3. level 顺序固定为 `project > global > admin > plugin`；同级 source 顺序 `claude > agents > codex > opencode > opencode-paths`；最后用 discovery path 稳定排序。Phase 1 不扫描 admin/plugin，但现在给出全序，避免 sort comparator 对冻结枚举返回相等。
4. 同 ID 多个不同 RealPath 生成 `different-content`。
5. frontmatter name 与目录 basename 不同生成 `frontmatter-name`。
6. 不同 ID 在同一 agent 上共享有效名生成一条 `effective-name`，IDs/Paths 排序稳定。
7. 输入 location/slice 不被就地修改。

API：

```go
func Build(locations []Location) ([]Skill, []Collision)
```

- [ ] **Step 4: 确认失败并实现最小逻辑**

Run: `go test ./internal/skill -run TestBuild -v`

Expected: FAIL。

实现时先深拷贝每个 `Names` map，再按 ID 分组；去重使用 `string(Source)+"\x00"+DiscoveryPath`。`Skill` 按 ID 排序，`Locations` 按上述优先级排序。碰撞也稳定排序，保证后续 CLI/golden 不抖动。

- [ ] **Step 5: 运行测试并提交**

Run: `gofmt -w internal/skill && go test ./internal/skill -v`

Expected: PASS。

```bash
git add internal/skill docs/superpowers/specs/2026-09-02-skill-scope-design.md
git commit -m "feat: define skill identity and collision model"
```

---

### Task 5: 数据驱动扫描 Claude 全局、项目 skill 与 legacy command

**Files:**

- Create: `internal/skill/scope.go`
- Create: `internal/skill/scan.go`
- Create: `internal/skill/scan_test.go`
- Modify: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`（补齐 malformed frontmatter 错误策略）

- [ ] **Step 1: 先补齐 malformed frontmatter 策略**

在 spec §4.3/§10 明确：有 frontmatter 起始分隔符但 YAML 非法、缺少结束分隔符或 `name` 不是字符串时 fail-closed，错误包含 `SKILL.md` discovery path；完全没有 frontmatter 或合法 frontmatter 缺少 `name` 时 `FrontmatterName` 为空。选择 fail-closed，因为 Codex/OpenCode 的有效名依赖该字段，静默回退会让白名单命中错误对象；Phase 1 先实现同一身份规则，避免后续改变扫描语义。

- [ ] **Step 2: 写扫描文件系统 seam 与结果类型**

`scan.go` 定义：

```go
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	EvalSymlinks(name string) (string, error)
}

type ScanResult struct {
	Skills      []Skill
	Collisions  []Collision
	ProjectRoot string
}

type Scanner struct {
	FS FileSystem
}

func (s Scanner) ScanClaude(env host.Env) (ScanResult, error)
```

`scope.go` 用私有 `scanRoot` 数据描述 root，不在遍历函数写 agent 分支：

```go
type scanRoot struct {
	Path   string
	Kind   Kind
	Level  Level
	Source Source
	Scope  string
}
```

Phase 1 只生成 `SourceClaude` 的 global/project roots；后续 phase 追加表行。

- [ ] **Step 3: 写 MapFS 扫描失败测试**

测试包装 `fstest.MapFS` 以满足 `FileSystem`；普通 fixture 的 `EvalSymlinks` 返回清理后的原路径。覆盖：

- 默认全局 `<home>/.claude/skills/foo/SKILL.md` 与 `CLAUDE_CONFIG_DIR` override，override trim 后空值回退默认。
- global `.claude/commands/review.md` 与嵌套 `git/commit.md`，ID 分别 `review`、`git:commit`。
- cwd 为 `<repo>/apps/web` 且 `<repo>/.git` 存在时，扫描 repo、apps、apps/web 每一级 `.claude/skills`；ID/Claude name 分别是 `root`、`apps:middle`、`apps/web:local`。
- 找不到 `.git` 时只扫 cwd；遇到嵌套仓库时以最近的 `.git` 为根。
- skill directory 只看直接 `SKILL.md`；commands 递归，但跳过 `.git`、`node_modules` 和 symlink 目录。
- 不存在的 root/文件跳过；root 已存在后出现 permission error 返回带 discovery path 的错误。
- skill entry 本身为 symlink 时允许读取直接 `SKILL.md`，`DiscoveryPath` 保持链接路径，`RealPath` 为解析后路径。
- YAML frontmatter `name` 提取；缺少 frontmatter/name 时为空；malformed YAML 返回扫描错误。
- `Names[AgentClaude]` 等于 ID，其他 agent key 不存在。

- [ ] **Step 4: 运行测试确认失败**

Run: `go test ./internal/skill -run TestScanner -v`

Expected: FAIL。

- [ ] **Step 5: 实现 scope 与 git 根查找**

路径内部统一使用 slash 形式：`filepath.ToSlash` 输入，`path.Join` 组合；`host.OSFileSystem` 在边界转回 native path。向上查找 `.git`：从 cwd 开始，遇到首个存在的 `.git`（文件或目录）即为 root；stat 只有 `fs.ErrNotExist` 可继续，其他错误 fail-closed；到卷根仍未找到则 `ProjectRoot=cwd` 且只返回 cwd 一层。

项目 scope 是当前扫描层相对 project root 的 slash 路径；root scope 为空。skill ID 是 `scope+":"+basename`（scope 空时只用 basename）；command ID 是 scope、command 相对 commands root 的目录和无扩展名 basename 用 `:` 拼接。

- [ ] **Step 6: 实现遍历与 frontmatter**

skill root：`ReadDir` 一次；普通目录或 symlink entry 都只尝试 `<entry>/SKILL.md`。普通文件忽略。读取成功后：

- `DiscoveryPath` 指向 `SKILL.md`，不是目录。
- `RealPath` 对完整 `DiscoveryPath`（含 `SKILL.md`）调用 `EvalSymlinks`；这样 entry directory 或 `SKILL.md` 文件任一层是 symlink/Junction 都能解析到最终文件。
- `FrontmatterName` 从开头两条 `---` 之间用 `yaml.Unmarshal` 到 `{Name string}`；没有 frontmatter 返回空；存在但无法解析返回错误。

普通目录中没有 `SKILL.md` 表示“该目录不是 skill”，按模式不匹配跳过；symlink entry 读取 `SKILL.md` 返回 `fs.ErrNotExist` 表示链接断裂，必须 fail-closed。其他读取错误一律返回。

command root：深度优先、目录项按 name 排序；symlink 目录、`.git`、`node_modules` 整棵跳过；接收 `.md` 普通文件和 `.md` symlink 文件，断链 fail-closed。每个 command 文件调用 `EvalSymlinks` 填 `RealPath`；`FrontmatterName` 为空，`Names` 只含 Claude ID。

任何 root 的第一次 `ReadDir` 返回 `fs.ErrNotExist` 都跳过；root 已进入后所有 I/O 错误都包装路径并返回。

- [ ] **Step 7: 实现真实 symlink 测试**

追加使用 `t.TempDir()`、`os.Symlink`、`host.OSFileSystem{}` 的黑盒测试；Windows 创建 symlink 权限不足时只 skip symlink case，其他扫描测试不能 skip。覆盖两个 fixture：同一真实目录通过两个 discovery entry 出现时，Build 保留两个 location 且 RealPath 相同；`SKILL.md` 文件自身是 symlink 时，RealPath 等于目标文件完整路径。

- [ ] **Step 8: 回归并提交**

Run:

```bash
gofmt -w internal/skill
go test ./internal/skill -v
go test ./internal/host ./internal/config ./internal/skill
```

Expected: PASS。

```bash
git add internal/skill docs/superpowers/specs/2026-09-02-skill-scope-design.md
git commit -m "feat: scan Claude skills and legacy commands"
```

---

### Task 6: 白名单解析的 `native` / `missing` 分支

**Files:**

- Create: `internal/skill/resolve.go`
- Create: `internal/skill/resolve_test.go`

- [ ] **Step 1: 定义可扩展但不越界的解析类型**

```go
type ResolutionState string
type ResolutionReason string

const (
	StateNative      ResolutionState = "native"
	StateProjected   ResolutionState = "projected"
	StateUnavailable ResolutionState = "unavailable"
	StateMissing     ResolutionState = "missing"
)

type Resolution struct {
	ID       string
	State    ResolutionState
	Names    []string
	Location *Location
	Reason   ResolutionReason
}

type Resolved struct {
	Agent   Agent
	Entries []Resolution
}

func ResolveNative(agent Agent, selected []string, inventory []Skill) Resolved
func (r Resolved) Count(state ResolutionState) int
func (r Resolved) AllowedNames() []string
```

`Location` 指针仅给 Phase 2 projected 分支使用；Phase 1 native/missing 都为 nil。这里不实现 unavailable/projected 的判定。

- [ ] **Step 2: 写失败测试**

覆盖：选中 ID 存在且有两个 Claude-visible locations 时 state=native、Names 稳定去重；未知 ID 为 missing；输出按 selection 首次出现顺序且去重；输入不变；AllowedNames 只汇总 native。不要构造“ID 存在但目标 agent 不可见”的输入：该情况属于 Phase 2 的 projected/unavailable 判定，不得在 `ResolveNative` 中误标为 missing。

Run: `go test ./internal/skill -run TestResolveNative -v`

Expected: FAIL。

- [ ] **Step 3: 实现、回归、提交**

实现只按 `Skill.ID` 建索引；Names 排序并去重。不要在这里读取 frontmatter、路径或配置。

Run: `gofmt -w internal/skill && go test ./internal/skill -v`

Expected: PASS。

```bash
git add internal/skill
git commit -m "feat: resolve native and missing skills"
```

---

### Task 7: 冻结 Agent 接口并实现 Claude Inventory

**Files:**

- Create: `internal/agent/agent.go`
- Create: `internal/agent/registry.go`
- Create: `internal/agent/registry_test.go`
- Create: `internal/agent/claude/adapter.go`
- Create: `internal/agent/claude/inventory.go`
- Create: `internal/agent/claude/inventory_test.go`
- Create: `internal/session/session.go`（最终路径 value object；Task 9 增加生命周期）
- Create: `internal/session/session_test.go`

- [ ] **Step 1: 写 Session 最终路径失败测试**

Create `internal/session/session_test.go`，使用 `package session_test`：构造 `Session{Root: finalRoot, Agent: skill.AgentClaude}`，断言 `AgentPath("settings.json")` 等于 `<finalRoot>/claude/settings.json`；断言传入的 parts slice 不被修改。该测试锁定 adapter 在 staging publish 前就必须拿到 final path 的契约。

Run: `go test ./internal/session -run TestSessionAgentPath -v`

Expected: FAIL，`internal/session` 或 `Session` 尚不存在。

- [ ] **Step 2: 实现 Session 最终路径 value object**

Create `internal/session/session.go`，让 Agent 接口可以编译，并让本步骤形成独立的红—绿提交单元：

```go
package session

type Session struct {
	Root  string
	Agent skill.Agent
}

func (s *Session) AgentPath(parts ...string) string {
	all := append([]string{s.Root, string(s.Agent)}, parts...)
	return filepath.Join(all...)
}
```

Task 9 会增加私有 staging/published 状态和行为，但不改变已经测试的 final Root/AgentPath 语义。

- [ ] **Step 3: 写 spec §7 的冻结类型**

Create `internal/agent/agent.go`：

```go
package agent

type Adapter interface {
	Name() skill.Agent
	Capabilities() Capabilities
	Inventory(ctx context.Context, env host.Env) (Inventory, error)
	Plan(resolved skill.Resolved, inv Inventory, sess *session.Session) (LaunchPlan, error)
}

type Capabilities struct {
	Projection    bool
	TogglePlugins bool
	ToggleBundled bool
}

type Inventory struct {
	Skills      []skill.Skill
	SkillNames  []string
	PluginIDs   []string
	Collisions  []skill.Collision
	Warnings    []string
}

type LaunchPlan struct {
	ControlArgs []string
	Env         map[string]string
	Files       []PlannedFile
}

type PlannedFile struct {
	Path string
	Data []byte
	Mode fs.FileMode
}
```

选择 `SkillNames` 作为 adapter 从 settings 收集的“扫描之外已有键”集合；选择 `PluginIDs` 作为后续三个 adapter 共用的 plugin 全集；`Collisions` 保留 scanner 的结构化碰撞，供 CLI 报告。`Warnings` Phase 1 为空，Phase 2+ 可承载非致命探测告警。所有返回 slice/map 都由实现新建。

同一步实现显式 registry：

```go
type Registry struct { /* map[skill.Agent]Adapter，私有 */ }

func NewRegistry(adapters ...Adapter) (Registry, error)
func (r Registry) Get(name skill.Agent) (Adapter, bool)
```

`registry_test.go` 用两个 fake adapter 覆盖成功查找、缺失与重复 Name 报错；不得提供全局 singleton 或 `init()` 注册。

- [ ] **Step 4: 写 Claude inventory 失败测试**

构造 fake scanner 与内存 ReadFileFS，覆盖：

- `Name()==AgentClaude`；Capabilities 固定 `{Projection:true, TogglePlugins:true, ToggleBundled:true}`，表示 Claude 能力，不表示 Phase 1 已启用所有分支。
- 调用 scanner 一次，深拷贝 scanner 的 Skills 与 Collisions。
- user settings 路径为 trim 后非空的 `$CLAUDE_CONFIG_DIR/settings.json`，否则 `<home>/.claude/settings.json`。
- project/local 路径为 scanner 返回的 `ProjectRoot/.claude/settings.json` 与 `settings.local.json`。
- 三层 `skillOverrides` key 求稳定并集，不使用 value 决定是否进入全集。
- 文件不存在跳过；非法 JSON、`skillOverrides` 非 object、value 非 string 均 fail-closed 且错误含路径。
- context 已取消时在 I/O 前返回 `context.Canceled`。

为了注入 scanner，`claude.Adapter` 使用小接口：

```go
type SkillScanner interface {
	ScanClaude(env host.Env) (skill.ScanResult, error)
}

type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

type Adapter struct {
	Scanner SkillScanner
	FS      ReadFileFS
}
```

不要复用 `config.ReadFileFS` 的名字来换取一行代码：Claude adapter 不应 import `internal/config`。两个包各自在消费处定义相同的最小结构接口，`host.OSFileSystem` 同时满足二者。

- [ ] **Step 5: 确认失败并实现**

Run: `go test ./internal/agent/claude -run TestInventory -v`

Expected: FAIL。

`settingsFile` 只声明 `SkillOverrides map[string]string`，未知 JSON 字段允许，因为 Claude settings 有大量与 skope 无关的合法键。使用 `json.Decoder` 解码并确认 EOF，拒绝尾随第二个 JSON 值。SkillNames 排序去重。

- [ ] **Step 6: 运行测试并提交**

Run:

```bash
gofmt -w internal/agent internal/session
go test ./internal/agent/... ./internal/session -v
```

Expected: PASS。

```bash
git add internal/agent internal/session
git commit -m "feat: define agent contract and Claude inventory"
```

---

### Task 8: 生成 Claude `settings.json` 与 `--settings` 规划

**Files:**

- Create: `internal/agent/claude/settings.go`
- Create: `internal/agent/claude/settings_test.go`
- Create: `internal/agent/claude/testdata/settings.golden.json`

- [ ] **Step 1: 写 golden 失败测试**

fixture inventory 包含扫描名 `alpha`、`beta`、`legacy` 和 settings 旧键 `stale`；resolved 只允许 `beta`、`legacy`。期望：

```json
{
  "skillOverrides": {
    "alpha": "off",
    "beta": "on",
    "legacy": "on",
    "stale": "off"
  }
}
```

文件必须以换行结尾。另测：空全集仍输出空 object；Plan 不修改 inventory/resolved；Plan 的 `Env` 是非 nil 空 map；Files 只有一个 `<session>/claude/settings.json`、mode `0o600`；ControlArgs 恰为 `--settings`, absolutePath。

- [ ] **Step 2: 确认失败并实现**

Run: `go test ./internal/agent/claude -run TestPlan -v`

Expected: FAIL。

`Adapter.Plan`：

1. inventory 中每个 Skill 的 Claude effective name、`SkillNames` 和 resolved allowed names 组成全集。
2. 全集默认 `off`，allowed 改 `on`。
3. 用 `encoding/json.MarshalIndent`；Go JSON 对 string map key 稳定排序。
4. 不写 `enabledPlugins`、`disableBundledSkills`，不使用 selection 的 plugin/bundled。

本 Task 使用 Task 7 已建立的 `session.Session.AgentPath`；Task 9 只补行为，不改变生成的最终 settings path。

- [ ] **Step 3: 运行测试并提交**

Run: `gofmt -w internal/agent internal/session && go test ./internal/agent/... -v`

Expected: PASS。

```bash
git add internal/agent/claude
git commit -m "feat: plan Claude skill override settings"
```

---

### Task 9: 会话 preview、staging 发布、owner 与残留回收

**Files:**

- Modify: `internal/session/session.go`
- Create: `internal/session/owner.go`
- Create: `internal/session/reap.go`
- Create: `internal/session/process.go`
- Create: `internal/session/process_linux.go`
- Create: `internal/session/process_darwin.go`
- Create: `internal/session/process_windows.go`
- Create: `internal/session/process_live_unix_test.go`
- Create: `internal/session/process_linux_test.go`
- Modify: `internal/session/session_test.go`

- [ ] **Step 1: 写 Session Manager 的失败测试**

以 fake clock、固定 random reader、fake `ProcessInspector` 覆盖：

- `Preview(claude, "dev")` 返回最终命名形状和 AgentPath，但磁盘无 `sessions/`。
- `Stage` 返回的 `Session.Root` 从一开始就是最终 `claude-yyyymmdd-hhmmss-<rand4>` 路径，同时在私有 `stagingRoot` 创建 mode 0700 目录与 mode 0600 `owner.json`；owner 有当前 PID/start token、agent、display name、UTC createdAt。
- `WriteFiles` 拒绝逃出 session root 的绝对/`..` path；创建父目录 0700、文件 0600；任一写失败后 `Abort` 可完整清理 staging。
- `WriteFiles` 接收的 Path 位于公开 final Root 下，但实际按相对路径写进 staging；这样 adapter 在 publish 前生成的 `--settings <final>/...` 在 rename 后仍有效。
- `Publish` 把 staging 原子 rename 到预先确定的 Root；final 已存在时失败且不覆盖，Root 不发生变化。
- Reap 删除 start token 不匹配或 PID 不存在的 owner session；保留 token 匹配的 live session。
- `.staging-*` 与无/坏 owner 的目录不足一小时保留，超过一小时删除。
- 单个目录读取/删除失败加入 warnings 并继续处理其他目录；sessions root 不存在返回空 warnings。
- 输入 plan Files/Data 不被修改。

权限位断言只在 `runtime.GOOS != "windows"` 执行；Windows 仍验证创建/清理行为，Phase 6 再验证 DACL/Job 相关语义。

API：

```go
var ErrProcessNotFound = errors.New("process not found")
var ErrUnsupported = errors.New("process inspection unsupported")

type ProcessInspector interface {
	StartToken(pid int) (string, error)
}

type OSProcessInspector struct{}

type File struct {
	Path string
	Data []byte
	Mode fs.FileMode
}

type Manager struct {
	Home      string
	Now       func() time.Time
	Random    io.Reader
	PID       int
	Processes ProcessInspector
}

func NewManager(home string) *Manager
func (m *Manager) Preview(agent skill.Agent, displayName string) (*Session, error)
func (m *Manager) Stage(agent skill.Agent, displayName string) (*Session, error)
func (m *Manager) Reap() []error
func (m *Manager) Write(sess *Session, files []File) error
func (m *Manager) Publish(sess *Session) error
func (m *Manager) Abort(sess *Session) error
func (s *Session) AgentPath(parts ...string) string
func (s *Session) WriteFiles(files []File) error
func (s *Session) Publish() error
func (s *Session) Abort() error
```

`session` 不能 import `agent`，所以 `File` 属于 session；`launch` 从 `agent.PlannedFile` 显式转换。最终依赖保持 `agent -> session`，而非双向。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/session -v`

Expected: FAIL。

- [ ] **Step 3: 实现 owner 与 staging/publish**

`owner.json` schema 固定：

```go
type Owner struct {
	PID          int         `json:"pid"`
	ProcessStart string      `json:"processStart"`
	Agent        skill.Agent `json:"agent"`
	SkillSet     string      `json:"skillSet"`
	CreatedAt    time.Time   `json:"createdAt"`
}
```

最终目录随机后缀从 2 bytes 编成 4 个小写 hex。`Preview` 消耗同样数量随机字节但不调用 mkdir/write；`Stage` 先确定 final Root，再确保 sessions root 0700、创建唯一 staging，写完整 owner，任何失败都清理 staging。`Session` 的 `stagingRoot`、`published` 均为私有字段。

`WriteFiles` 要求每个 Path 已在 Session.Root 下；用 `filepath.Rel` 后拒绝 `.`、`..` 或以 `..+separator` 开头，再把该 relative path 接到私有 stagingRoot。Phase 1 adapter 传绝对 final path。忽略 PlannedFile 自带 mode 的其他值，目录固定 0700、文件固定 0600，以 spec 权限为准。`Abort` 在 publish 前删 staging，publish 后只删经过 sessions-root 子目录校验的 final Root。

- [ ] **Step 4: 实现平台 ProcessInspector**

`process_linux.go`（`//go:build linux`）把 OS 读取与 stat 解析分开，提供可供黑盒 fixture 驱动的生产 seam：

```go
type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

type LinuxProcessInspector struct {
	FS ReadFileFS
}

func (p LinuxProcessInspector) StartToken(pid int) (string, error)
```

`LinuxProcessInspector` 读取 `/proc/<pid>/stat`，从最后一个 `") "` 后的字段取 Linux stat 第 22 字段 starttime；文件不存在映射到 `ErrProcessNotFound`。`OSProcessInspector.StartToken` 只委托给 `LinuxProcessInspector{FS: osReadFileFS{}}`，其中私有 `osReadFileFS` 包装 `os.ReadFile`。这样 `process_linux_test.go` 可以保持 `package session_test`，用 fake ReadFileFS 通过公开 `StartToken` 覆盖带空格/右括号进程名，无需导出仅供测试的 parser 或改用白盒测试。

`process_darwin.go`（`//go:build darwin`）：调用 `unix.SysctlKinfoProcSlice("kern.proc.pid", pid)`；空 slice 映射到 `ErrProcessNotFound`，一项时 token 用 `items[0].Proc.P_starttime.Sec` 与 `Usec`，多于一项视为内部错误。不要使用 `SysctlKinfoProc` 加“只判断 ESRCH”的方案：x/sys v0.41.0 会把空结果长度转换成 `EIO`，导致退出 PID 无法回收。

`process_windows.go`（`//go:build windows`）：`OSProcessInspector.StartToken` 返回 `ErrUnsupported`。Phase 6 用 Windows API 替换，不在本期猜测实现。

`process_live_unix_test.go`（`//go:build linux || darwin`）用 `os.Getpid()` 断言当前进程 token 非空；再启动并等待一个立即退出的子进程，以该已退出 PID 断言 `errors.Is(err, ErrProcessNotFound)`，不使用可能碰巧存在的硬编码 PID。`process_linux_test.go`（`//go:build linux`、`package session_test`）用 fake ReadFileFS 驱动 `LinuxProcessInspector.StartToken`，覆盖带空格/右括号的进程名、字段不足和 `fs.ErrNotExist`。

- [ ] **Step 5: 实现 fail-safe 回收**

owner 可解析时：`StartToken` 返回 not found 或 token 不同才删除；返回其他错误只 warning 并保留。owner 不存在/损坏按目录 mtime 的一小时规则处理。所有删除目标都必须先 `filepath.Rel(sessionsRoot, target)` 验证是单层子目录，禁止递归删除 root 或计算后未验证的路径。

- [ ] **Step 6: 运行单测与提交**

Run:

```bash
gofmt -w internal/session
go test ./internal/session -v
```

Expected: PASS；Windows 上 OS inspector 的生产路径不参与 Stage 单测，测试全部注入 fake inspector。

```bash
git add internal/session
git commit -m "feat: manage session staging and stale cleanup"
```

---

### Task 10: Unix `syscall.Exec` handoff 与 Windows 明确拒绝

**Files:**

- Create: `internal/handoff/handoff.go`
- Create: `internal/handoff/handoff_unix.go`
- Create: `internal/handoff/handoff_windows.go`

- [ ] **Step 1: 定义消费方可注入的 concrete API**

```go
package handoff

var ErrUnsupported = errors.New("handoff is not implemented on this platform")

type Handoff struct{}
```

`handoff.go` 只放 sentinel 与类型；`Exec(path string, args, env []string) error` 方法分别在两个平台文件实现，不能在公共文件再写一份无 body 声明。

`handoff_unix.go` 使用 `//go:build unix`：

```go
func (Handoff) Exec(executable string, args, env []string) error {
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, executable)
	argv = append(argv, args...)
	return syscall.Exec(executable, argv, env)
}
```

`handoff_windows.go` 返回 `ErrUnsupported`。接口由消费方 `launch` 定义，handoff 包不定义多余 interface。

- [ ] **Step 2: 交叉编译验证**

Run:

```bash
go test ./internal/handoff
GOOS=linux GOARCH=amd64 go build ./internal/handoff
GOOS=darwin GOARCH=arm64 go build ./internal/handoff
GOOS=windows GOARCH=amd64 go build ./internal/handoff
```

Expected: native test 与三个交叉编译均退出 0；library package 的 `go build` 只写 Go build cache，不在仓库留下异平台 binary。不要在单元测试进程内直接调用成功的 `syscall.Exec`。

- [ ] **Step 3: 提交**

```bash
git add internal/handoff
git commit -m "feat: add Unix agent handoff"
```

---

### Task 11: `launch.Service` 编排 Phase 1 的完整十步流程

**Files:**

- Create: `internal/launch/launch.go`
- Create: `internal/launch/render.go`
- Create: `internal/launch/launch_test.go`

- [ ] **Step 1: 定义请求、结果与依赖接口**

`launch` 作为消费方定义接口：

```go
type AdapterRegistry interface {
	Get(skill.Agent) (agent.Adapter, bool)
}

type ExecutableResolver interface {
	LookPath(command string) (string, error)
}

type Handoff interface {
	Exec(path string, args, env []string) error
}

type SessionManager interface {
	Reap() []error
	Preview(skill.Agent, string) (*session.Session, error)
	Stage(skill.Agent, string) (*session.Session, error)
	Write(*session.Session, []session.File) error
	Publish(*session.Session) error
	Abort(*session.Session) error
}

type Request struct {
	Agent       skill.Agent
	SetValue   string
	SetPresent bool
	DryRun     bool
	AgentArgs  []string
}

type Result struct {
	Executable string
	Args       []string
	Env        []string
	Inventory  agent.Inventory
	Resolved   skill.Resolved
	Plan       agent.LaunchPlan
	Session    *session.Session
	Warnings   []error
	NoIsolation bool
}

type Reporter func(Result) error

type Service struct {
	Env       host.Env
	FS        config.ReadFileFS
	SkopeHome string
	Registry  AdapterRegistry
	Resolver  ExecutableResolver
	Sessions  SessionManager
	Handoff   Handoff
}

func (s *Service) Run(ctx context.Context, req Request, report Reporter) error
```

`session.Manager.Write/Publish/Abort` 是给消费方注入的薄委托，分别调用 Session 同名行为；增加这些方法是为了 launch 单测能记录完整副作用顺序，不改变 session 的职责。

生产 registry 用 `map[skill.Agent]agent.Adapter` 的小 struct，在 CLI 显式构造，不用 `init()`。

- [ ] **Step 2: 写记录型依赖的顺序测试**

`launch_test.go` 的 fakes 把调用名 append 到 `[]string`，分别断言：

**活动 set 成功：**

```text
load config -> reap -> lookpath -> load skillsets -> merge -> inventory ->
resolve -> stage/preview -> adapter plan -> write -> publish -> handoff
```

纯函数 load/merge/resolve 不易记录时，用结果断言；副作用顺序必须记录。另覆盖：

- config 损坏时不 reap/lookpath；reap warning 不阻断。
- command 选择：`config.agents.claude.command` 非空优先，否则 `claude`。
- argv 精确为 config args + request AgentArgs + plan ControlArgs。
- `-s none` 不加载 skillsets、不 inventory、不 session、不 plan，argv 仍含 config args + passthrough。
- active set 缺失/损坏、未知 set、inventory error、stage/write/publish/plan error 均 fail-closed；stage 后失败调用 Abort。
- dry-run 用 Preview，只调用 reporter，不 Write/Publish/Handoff；none dry-run 连 Preview 也不调用。
- handoff 返回错误时删除已发布 session；清理失败作为 warning 附加但保留原 handoff error。
- reporter 在 handoff 前被调用一次；reporter 失败时不 handoff，并清理已 staging/published 的 session。
- context cancellation 在每个外部步骤前检查。

- [ ] **Step 3: 确认失败**

Run: `go test ./internal/launch -v`

Expected: FAIL。

- [ ] **Step 4: 实现流程**

`Run` 精确顺序：

1. `config.Load(<home>/config.toml)`；这是 `none` 也必须经过的门禁。
2. `Sessions.Reap`，错误追加 warnings。
3. command default/override 后 `Resolver.LookPath`；错误包装并提示 `[agents.claude] command`。
4. 未传 set 返回类型化 `ErrSetRequired`。
5. `ParseSelection`；单独 `none` 直接组装 Result，调用 reporter；dry-run 返回，真实运行再 handoff。
6. `LoadSkillSets`，不存在时按 active selection 报 path 错误；Merge。
7. registry 获取 adapter，Inventory；`ResolveNative`。Result 保留 Inventory，以便 reporter 输出结构化碰撞。
8. dry-run Preview，否则 Stage；Adapter.Plan。
9. 组装 args/env/result：环境必须用 `s.Env.With(plan.Env).Environ()`，`none` 使用原始 `s.Env.Environ()`；dry-run 调 reporter 后返回。真实运行转换 `agent.PlannedFile -> session.File`、Write、Publish，再调用 reporter。
10. Handoff。Unix 成功不返回；若返回错误则 Abort final session并返回错误。

`config.AgentConfig.Args`、request args、plan args 组装时都复制，禁止 append 到调用者 slice 的底层数组。

必须在 launch 内调用 reporter，因为成功的 Unix `syscall.Exec` 永不返回；如果 CLI 等 `Run` 返回后才渲染，真实启动永远看不到 summary。Reporter 只接收结构化 Result，launch 不拼用户文案。

- [ ] **Step 5: Phase 1 最小 render 数据**

`render.go` 只提供结构化摘要，CLI 再写文本：

```go
type Summary struct {
	Native  int
	Missing []string
}

func Summarize(resolved skill.Resolved) Summary
```

本期不实现 §9.1 转义和完整 §6.6 plugin/bundled/projected 行。

- [ ] **Step 6: 回归并提交**

Run:

```bash
gofmt -w internal/launch
go test ./internal/launch -v
go test ./internal/agent/... ./internal/session ./internal/launch
```

Expected: PASS。

```bash
git add internal/launch
git commit -m "feat: orchestrate the Phase 1 launch flow"
```

---

### Task 12: Agent 参数切分

**Files:**

- Create: `internal/cli/args.go`
- Create: `internal/cli/args_test.go`

- [ ] **Step 1: 写完整 table test**

私有结果：

```go
type parsedLaunchArgs struct {
	setValue   string
	setPresent bool
	dryRun     bool
	agentArgs  []string
}

func parseLaunchArgs(args []string) (parsedLaunchArgs, error)
```

case：

| argv | result |
|---|---|
| `-s dev` | set dev |
| `--set dev` | set dev |
| `--set=dev` | set dev |
| `-s=dev` | set dev |
| `--dry-run -s dev` | dryRun + set |
| `-s dev -- --model x` | passthrough `--model x`，不含 `--` |
| `-s dev --model x -s other` | 第一个未知参数起全部 passthrough，后一个 `-s` 不算重复 |
| `prompt text` | 全部 passthrough，set absent |
| `-s` / `--set` | missing value error |
| `-s dev --set other` | repeated set error |
| `--set=` | empty value error |

测试还断言输入 slice 不变。

- [ ] **Step 2: 确认失败并实现单趟 parser**

Run: `go test ./internal/cli -run TestParseLaunchArgs -v`

Expected: FAIL。

parser 只在遇到未知 token 或 `--` 之前识别 skope 参数。错误定义为类型化 sentinel/struct，用户文案在 launch command 拼。

- [ ] **Step 3: 回归并提交**

Run: `gofmt -w internal/cli && go test ./internal/cli -run TestParseLaunchArgs -v`

Expected: PASS。

```bash
git add internal/cli/args.go internal/cli/args_test.go
git commit -m "feat: parse scoped agent launch arguments"
```

---

### Task 13: `skope list` 管理命令

**Files:**

- Create: `internal/cli/list_cmd.go`
- Create: `internal/cli/list_cmd_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

- [ ] **Step 1: 写 handler 失败测试**

为了避免测试真实 home，命令内部使用闭包：

```go
type listLoader func() (path string, sets config.SkillSets, err error)
func newListCmd(load listLoader) *cobra.Command
```

保持 `package cli_test` 黑盒测试，不能调用私有 root factory。Task 13 把 `root.go` 重构为可注入 Application：

```go
type Application struct {
	LoadSkillSets func() (path string, sets config.SkillSets, err error)
}

func (a Application) Execute(args []string, stdout, stderr io.Writer, version string) int
```

现有顶层 `cli.Execute` 继续构造生产 Application 并委托，`cmd/skope/main.go` 不变。测试构造 Application 注入 loader，覆盖：

- 不存在：输出精确包含 path 与 `暂无配置`，exit 0。
- 两个 set：按配置顺序输出名字、描述、skills 数、Claude/Codex plugin 数、bundled `on/off`。
- 空描述仍保持一行；损坏返回错误，exit 1；不接收位置参数。
- root help 列出 `list` 与 `claude`（claude Task 14 接入前可先只断言 list，Task 14 更新断言）。

表头固定：

```text
NAME  DESCRIPTION  SKILLS  CLAUDE PLUGINS  CODEX PLUGINS  BUNDLED
```

用 `text/tabwriter`，测试断言字段而非空格数量；检查每次写入和 `Flush` 的 error，输出失败必须让命令返回 1。

- [ ] **Step 2: 确认失败并实现**

Run: `go test ./internal/cli -run 'TestList|TestExecuteHelp' -v`

Expected: FAIL。

生产 loader 每次命令运行时才 Snapshot/ResolveHome/LoadSkillSets；`--help` 不得读取 home 或配置。`list` 不读 `config.toml`，不创建目录。Application 的 nil loader 只允许 help/version 测试；执行 `list` 时必须返回明确的内部配置错误，不能 panic。

- [ ] **Step 3: 回归并提交**

Run: `gofmt -w internal/cli && go test ./internal/cli -v`

Expected: PASS。

```bash
git add internal/cli
git commit -m "feat: list configured skill sets"
```

---

### Task 14: `skope claude` 命令、生产依赖装配与最小输出

**Files:**

- Create: `internal/cli/launch_cmd.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Verify only: `cmd/skope/main.go`（Execute 外部签名不变，不应产生 diff）

- [ ] **Step 1: 写命令层失败测试**

`newLaunchCmd(agent, runner)` 接受：

```go
type launchRunner func(context.Context, launch.Request, launch.Reporter) error
```

测试覆盖：

- command `Use: "claude [skope options] [agent args...]"`、`DisableFlagParsing:true`、`Args:cobra.ArbitraryArgs`。
- parse 结果完整传给 Request；command 提供 reporter，runner error 令 Execute 返回 1 且 stderr 有上下文。
- 成功活动 set 输出 `Skillset [dev] for claude`、native 数、missing ID 和 `Launching claude`。
- Result.Inventory 含碰撞时逐条输出 kind、ID/name 与 discovery path；不丢弃 `different-content`、`frontmatter-name` 或 `effective-name`。
- dry-run 输出 executable、最终 argv、`claude/settings.json` 相对路径与生成 JSON；不打印环境变量值（Phase 1 plan.Env 为空）。
- `none` 输出 `Skillset [none] for claude` 和 no-isolation 标记，不输出 native/missing。

- [ ] **Step 2: 确认失败并实现命令**

Run: `go test ./internal/cli -run TestClaudeCommand -v`

Expected: FAIL。

渲染使用 `cmd.OutOrStdout()`；错误经 `RunE` 返回。Phase 1 尚未接 `termsafe`，测试不要塞控制字符并假装安全需求已满足。

- [ ] **Step 3: 实现显式生产装配**

`root.go` 增加私有 `dependencies`，默认构造顺序：

1. `host.Snapshot()` 与 `config.ResolveHome` 延迟到真正执行命令。
2. 单个 `host.OSFileSystem{}` 同时提供 config/skill/Claude settings 的只读接口。
3. `skill.Scanner{FS: osfs}`。
4. `claude.Adapter{Scanner: scanner, FS: osfs}`。
5. 显式 registry map 只注册 Claude。
6. `session.NewManager(skopeHome)`。
7. executable resolver 是 `host` 包中的薄 `exec.LookPath` 适配器；新增 `internal/host/exec.go`，让 launch 不直接读宿主状态。
8. `handoff.Handoff{}`。

不得用 `init()` 注册，不得让 adapter import cli/launch。

Task 13 的公开 `Application` 增加字段：

```go
type Application struct {
	LoadSkillSets func() (path string, sets config.SkillSets, err error)
	RunLaunch     func(context.Context, launch.Request, launch.Reporter) error
}
```

命令测试通过 `Application.Execute` 注入 fake runner；fake runner 主动调用 reporter，才能覆盖 active launch 在 Unix exec 前的渲染。生产 Application 的 RunLaunch 委托给 `launch.Service.Run`。

`Execute` 仍保持 Phase 0 的外部签名 `Execute(args []string, stdout, stderr io.Writer, version string) int`，因此 `cmd/skope/main.go` 原则上不改。生产 env/cwd 在 command 真正运行时 Snapshot；测试通过 `Application.Execute` 注入 runner/loader。

- [ ] **Step 4: 根命令回归**

Run:

```bash
gofmt -w cmd/skope internal/cli internal/host
go test ./internal/cli ./internal/host -v
go run ./cmd/skope --help
go run ./cmd/skope help claude
```

Expected: root help 同时列出 `claude`、`list`、`version`；`help claude` 显示 skope launch usage。由于 `DisableFlagParsing=true`，`skope claude --help` 是 Claude passthrough 参数，不作为 skope 帮助入口。

- [ ] **Step 5: 提交**

```bash
git add internal/cli internal/host
git commit -m "feat: expose the Claude launch command"
```

---

### Task 15: fake agent Unix 端到端与本期错误行

**Files:**

- Modify: `internal/testutil/build.go`
- Modify: `internal/testutil/build_test.go`
- Create: `internal/cli/integration_test.go`

- [ ] **Step 1: 增加 skope 二进制构建辅助**

在 `internal/testutil/build.go` 抽出私有 `buildPackage`，新增：

```go
func BuildSkope(tb testing.TB) string
```

它与 fake agent 分别 `sync.Once` 缓存，构建 `github.com/scarb/skope/cmd/skope`；Windows 后缀正确。`testing.Short()` 的调用方负责 skip。`build_test.go` 运行 `skope version` 断言 `skope dev`。

- [ ] **Step 2: 写 Unix 端到端失败测试**

`integration_test.go` 使用 `package cli_test`；`testing.Short()` 或 `runtime.GOOS == "windows"` 时 skip。每个测试：

1. BuildSkope 与 BuildFakeAgent。
2. 创建临时 home/repo；repo 有 `.git`。
3. 写 `SKOPE_HOME/config.toml`，Claude command 指向 fake agent，args 为 `--from-config configured`。
4. 写 `skillsets.toml`：dev 选择 `allowed`，不选择 `blocked`。
5. 写 `$CLAUDE_CONFIG_DIR/skills/{allowed,blocked}/SKILL.md` 和已有 settings key `stale`。
6. 子进程 cwd=repo。测试内实现 `withEnv(base []string, overrides map[string]string) []string`：保留 `os.Environ()` 中的 `HOME`、`PATH` 等变量，先删除与 override 同名的旧条目，再按 key 排序追加 `SKOPE_HOME`、`CLAUDE_CONFIG_DIR`、`FAKEAGENT_OUT`、`FAKEAGENT_EXIT`；禁止简单 append 产生重复 key，也禁止只传 fixture 变量导致 `os.UserHomeDir` 失败。

断言：

- `skope claude -s dev --from-user value` 最终 fake argv 顺序是 config args、user args、`--settings <path>`。
- 设置 `FAKEAGENT_EXIT=23` 时，运行 skope 的父测试观察到的最终退出码也是 23；这条断言验证 Unix `syscall.Exec` 的退出码透传，复用 Phase 0 fakeagent 已有能力。
- settings JSON 中 allowed=on，blocked/stale=off，且无 enabledPlugins/disableBundledSkills。
- owner/session/final path 存在，权限在 Unix 为目录 0700、文件 0600。
- fake 的 cwd 等于 repo，环境仍含调用者变量。
- 第二次执行 `skope claude -s dev --dry-run` 会回收第一次已退出 PID 的 session，且不创建新 session、不改写 fake output。
- `-s none` 即使 `skillsets.toml` 损坏也启动；argv 无 `--settings`，无新 session。

- [ ] **Step 3: 写错误路径集成测试**

至少覆盖 spec §10 本期行：

- config.toml 损坏（含 `-s none`）退出 1。
- active set 下 skillsets 缺失/损坏、set 不存在退出 1。
- command 不存在退出 1，错误提示配置 `[agents.claude] command`。
- 扫描 root 已进入后的 I/O 错误在 Unix 可通过权限 fixture 稳定制造时测试；以 root/CI 用户无法制造 EACCES 时保留单元测试作为证据，不写脆弱集成断言。
- 未传 `-s` 退出 1并提示显式传 `-s <name>` 或 `-s none`。
- missing ID 只告警，fake agent 仍启动。
- dry-run 不启动 fake agent。

- [ ] **Step 4: 运行红灯后实现缺口**

Run: `go test ./internal/cli -run Integration -v`

Expected before fixes: FAIL；逐个修到 PASS。不得为通过测试放宽 fail-closed 规则。

- [ ] **Step 5: 全量测试并提交**

Run:

```bash
gofmt -w internal/testutil internal/cli
go test ./...
go test -short ./...
```

Expected: PASS；Windows 只 skip Unix exec integration，其他单测与 BuildSkope 测试通过。

```bash
git add internal/testutil internal/cli
git commit -m "test: cover the Claude launch end to end"
```

---

### Task 16: 类型冻结审计、真实 Claude 验证与 Phase 1 收尾

**Files:**

- Modify: `docs/verification.md`
- Modify: `docs/superpowers/plans/2026-09-04-phase1-claude-minimal-loop.md`（勾选执行项并记录偏差）
- Modify: `README.md`（只更新 status 与可运行示例）

- [ ] **Step 1: 类型冻结审计**

逐字段对照 spec：

| 类型 | 对照 |
|---|---|
| `skill.Skill` / `skill.Location` | §4.2 全字段、Names key 类型、Location 顺序约定 |
| `agent.Adapter` | §7 的四个方法与 `context.Context`/`host.Env`/`skill.Resolved`/`session.Session` |
| `agent.Capabilities` | Projection/TogglePlugins/ToggleBundled |
| `agent.LaunchPlan` | ControlArgs/Env/Files |
| `agent.Inventory` | Skills、有效名全集、plugin 全集及后续 warning 扩展点 |

若代码与 spec 不一致，先判断是实现错误还是设计必须变更；设计变更必须先修改 spec，再改代码并在 plan 记录原因。不得在 Task 16 静默改冻结类型。

- [ ] **Step 2: 本地质量门禁**

Run:

```bash
make check
go test -short ./...
go test -count=1 -coverprofile=coverage.txt ./...
go tool cover -func=coverage.txt
git status --short
```

Expected:

- gofmt、vet、golangci-lint、全部 test、build 退出 0。
- 输出总覆盖率并列出低覆盖业务函数；80% 是 spec §12 的项目目标，不是 Phase 1 的额外硬门禁。所有本期新增的分支型业务 seam 必须有行为测试；不得为追数字测试无业务逻辑的 `main`、平台声明或用排除规则掩盖缺口。
- status 只有本 Task 预期文档改动与用户原有 `?? AGENTS.md`；不得 stage `AGENTS.md`、`coverage.txt`、binary 或 `dist/`。

- [ ] **Step 3: CI 门禁**

推送 `codex/phase1-claude-minimal-loop`（或执行时约定的功能分支），创建/更新一个以 `main` 为 base 的 Pull Request，再确认 GitHub Actions：Ubuntu/macOS `go test -race`、Windows `go test`/build、Ubuntu lint 全部通过。当前 `.github/workflows/ci.yml` 的 `push` 只监听 `main`，功能分支必须通过 `pull_request` 事件触发；不要把“仅 push 功能分支”当作 CI 已执行。记录 run URL；失败先修复并重新执行本地门禁。

- [ ] **Step 4: 真实 Claude 手工验证**

在 Linux/macOS（或 WSL 中安装的独立 Claude）使用一次性目录，绝不覆盖真实配置；当前 Windows 主机不能代替 Unix handoff 验收。fixture 至少有 `allowed` 与 `blocked` 两个 skill；复制用户当前 Claude settings 时只复制实验所需最小字段。执行：

```bash
CLAUDE_CONFIG_DIR=<fixture>/claude \
SKOPE_HOME=<fixture>/skope \
./skope claude -s phase1
```

在 Claude 会话内分别确认：allowed 可调用；blocked 不可调用；退出后 settings 路径仍在 session 目录；下一次 dry-run 回收旧 session。把日期、OS、Claude 完整版本、fixture 布局、完整命令、观察和结论追加到 `docs/verification.md` 的 `Phase 1`，不把用户真实路径、token 或 settings 内容写入仓库。

此手工项不是 spec §7.5 第 3/10 条；不要提前把那两条标成通过。它只证明 Phase 1 依赖的 `--settings` + `skillOverrides` 闭环。

- [ ] **Step 5: 更新 README 与执行状态**

README status 改为 Phase 1 complete；增加两条最小示例：

```sh
skope list
skope claude -s dev -- <claude args>
```

明确写 Phase 1 仅支持 Linux/macOS Claude 原生 skills，plugin/bundled/projection/Windows handoff 尚未实现，避免用户把阶段性交付误解为完整 v1。

- [ ] **Step 6: 提交收尾文档**

```bash
git add README.md docs/verification.md docs/superpowers/plans/2026-09-04-phase1-claude-minimal-loop.md
git commit -m "docs: close phase 1 after Claude verification"
```

- [ ] **Step 7: 最终干净 clone 验证**

在 `mktemp -d` 创建的一次性 clone 中运行 `make check` 和一个 dry-run fixture；确认不依赖工作区未跟踪文件。验证完删除一次性 clone，不删除工作仓库或用户 home 下任何目录。

---

## Phase 1 验收矩阵

| spec 要求 | Task | 自动证据 |
|---|---:|---|
| §4.1 Claude global/project/command 扫描 | 5 | `internal/skill/scan_test.go` |
| §4.2/§4.3 身份、合并、有效名、碰撞 | 4–5 | `identity_test.go`、`scan_test.go` |
| §4.4 native/missing | 6 | `resolve_test.go` |
| §5.1 SKOPE_HOME / 文件缺失语义 | 2–3 | `home_test.go`、config tests |
| §5.2/§5.3 全部只读校验 | 2–3 | config table tests |
| §5.5 多 set 并集 | 3 | `TestMerge*` |
| §6.1 参数切分、none、argv 顺序 | 11–12、15 | args/unit/integration |
| §6.3 Phase 1 dry-run | 11、14–15 | launch/CLI/integration |
| §6.4 list | 13 | `list_cmd_test.go` |
| §7 类型与 Claude skillOverrides | 7–8 | inventory/settings golden |
| §8.1 编排 | 11 | 记录型依赖顺序测试 |
| §8.2 session/owner/reap | 9、15 | session unit + integration |
| §8.4 Unix exec 与退出码透传 | 10、15 | built skope → `FAKEAGENT_EXIT=23` |
| §10 本期错误行 | 2–3、5、9、11、15 | unit + integration |
| §12 fakeagent 集成 | 15 | `integration_test.go` |
| §14.1 类型冻结 | 4、7、16 | 审计表 + spec 对照 |
| §14.7 门禁 | 16 | make/CI/verification log |

## 明确不在本计划实现

- Claude plugin inventory、`enabledPlugins`、`disableBundledSkills`。
- 外来 skill projection、`--add-dir`、自包含检查。
- §6.2 冲突参数检测。
- 完整 §6.6 摘要和 §9.1 termsafe/脱敏。
- TTY 选择器、create/edit/delete/skills/doctor。
- Codex/OpenCode adapter。
- Windows Job Object、Ctrl 处理、`.cmd` shim 与正常退出清理。

这些能力分别保留在 spec Phase 2–6；执行 Phase 1 时发现相关测试需求，只记录后续事项，不把实现偷偷塞进本期。
