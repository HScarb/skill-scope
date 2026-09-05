# skill-scope Phase 2（Claude 补全）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 在 Phase 1 的 Claude 启动流程上补齐 plugin/bundled 白名单、外来 skill 投影、冲突参数检测、终端安全输出和完整 dry-run，使 Claude 达到 spec §14.2 的验收要求。

**Architecture:** 保留已冻结的六个核心类型，由 CLI 在每次启动解析出 command 和 skill set 后构造该次使用的 Claude adapter；adapter 内部保存不可变的 plugin/bundled 选项。`proc` 负责受限的辅助子进程，`projection` 负责只读检查与逐文件复制，`launch` 统一处理解析、staging、发布和失败清理，`cli` 在输出边界转义外部文本。

**Tech Stack:** 沿用 `go.mod` 的 Go 1.24.0、Cobra v1.10.2、go-toml/v2 v2.4.3、yaml.v3 v3.0.1、x/sys v0.41.0；新增能力优先使用标准库，不新增 UI 或配置库。

---

**Spec 对应：** `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §4.1/§4.4、§5.3、§6.2/§6.3/§6.6、§7.1/§7.5、§8.3、§9、§10、§11.1/§11.2、§14.2/§14.7。

**执行状态：** 2026-09-05 Task 1/2 的真实 Claude 第 3、10 条门禁通过，实际偏差已修订 spec；证据见 `docs/verification.md`。Task 3–16 可进入实施。Task 2 最后一次模型调用遇 provider 403 额度不足，后续真实模型验收需恢复额度；已有 init 路径证据和此前成功的控制组保留。

**审查修订（2026-09-05）：** Task 9/14/16 补外来入口读取前的类型检查、有界读取及拒绝原因传递；Task 10–12 补大小写冲突与投影文件禁止覆盖写入；Task 17 的真实验收绑定本次提交构建出的绝对路径。执行复选框按实际完成情况更新。

**执行技能：** 实施按用户指示使用 `@superpowers:subagent-driven-development`，编码参考 `@karpathy-guidelines`；新增行为按 `@superpowers:test-driven-development`；出现失败用 `@superpowers:systematic-debugging`；Task 17 使用 `@superpowers:verification-before-completion`。步骤中的命令在仓库根运行；含 POSIX 环境赋值的真实实验只在 Linux/macOS/WSL shell 运行。

## 代码基线与必须先解决的问题

- 基线为 `main` 的 `27d22be`，Phase 1 PR 已合并。规划时运行 `go test -short ./...`，所有包通过；这不是 Phase 2 实现证据。
- 当前工作区只有用户原有的未跟踪 `AGENTS.md`。本计划放在 Repository Guidelines 指定的 `docs/superpowers/plans/`，覆盖技能默认的 `docs/plans/`。只提交明确属于任务的路径，不使用 `git add .`。
- 计划进入版本控制后，实施前使用 `@superpowers:using-git-worktrees` 创建 `codex/phase2-claude-completion`。继承原工作区的约束，不把未跟踪的 `AGENTS.md` 加入提交。
- 仓库没有 `.codegraph/`；直接使用 `rg` 与精确文件读取，不为实施自动建索引。

| 现有代码 | 实际行为 | Phase 2 接法 |
|---|---|---|
| `internal/agent/agent.go` | `Adapter` 四方法；`Inventory` 只有 Skills/SkillNames/PluginIDs/Collisions/Warnings | 不加方法或字段，不把选择结果藏入 `host.Env` |
| `internal/cli/root.go:dependencies.runLaunch` | 读配置前固定构造 `claude.Adapter{Scanner, FS}` | 注入工厂，配置解析后构造该次 adapter；registry 仍由 CLI 显式建立 |
| `internal/agent/claude/inventory.go:readSkillNames` | 只读取三层 `skillOverrides` | 同次读取收集 `enabledPlugins`；一次 plugin list 获得安装清单 |
| `internal/agent/claude/settings.go:Plan` | 只写 `skillOverrides`，不使用已校验的 Plugins/Bundled | 从 adapter 的不可变 Options 读取这两个选择 |
| `internal/skill/scan.go:ScanClaude` | 只有 Claude 原生入口；`Names` 写死 Claude | 保留原入口，增加显式 root 扫描与外来来源入口 |
| `internal/skill/resolve.go:ResolveNative` | 已存在但没有目标 agent 名字的 ID 仍标 native；AllowedNames 只收 native | 替换为四状态解析；不能原样复用到混合 inventory |
| `internal/session/session.go:WriteFiles` | final path 映射到私有 staging；`os.Root` 锚定写入 | 每个投影文件复用此入口，补空目录写入，不暴露 staging 路径 |
| `internal/launch/launch.go:cloneResult` | 深复制 reporter 数据，Session 只给展示快照 | 新增投影路径元数据也深复制；不向 reporter 暴露复制句柄 |
| `internal/cli/launch_cmd.go:reportDryRun` | 打印 Plan.Files 中每个文件全文 | Plan.Files 继续只放生成配置；投影文件只列相对路径 |
| `internal/cli/root.go:newRootCmd` | Cobra 自行打印错误，`SilenceErrors:false` | 改为单一、经过 termsafe 的错误出口 |
| `internal/testutil/fakeagent/main.go:run` | 任意调用都写 FAKEAGENT_OUT | 新增 plugin list 分支；探测不能覆盖真正启动记录 |

## 本期决策与边界

1. **真实验证先行。** Task 1、2 是实施门禁；两项都通过或先修订冲突的 spec 后，才进入 Task 3–16。可以写计划、准备 fixture，不能将未验证 schema 写成真实事实。
2. **保留冻结类型。** `Skill`、`Location`、`Adapter`、`Capabilities`、`LaunchPlan`、`Inventory` 不改结构。启动工厂、`claude.Options`、`launch.Result`、`skill.Resolve` 是未冻结的实现入口。Options 同时供 Inventory 的 missing-plugin 告警和 Plan 使用，避免二次探测或 adapter 缓存可变 inventory。
3. **最小外来来源闭环。** 本期提前启用 `~/.agents/skills` 与 `$CODEX_HOME/skills`（默认 `~/.codex/skills`）两条静态全局扫描行，仅作为投影候选。它们各自的 Names 按 spec §4.3 填写；不因此注册 Codex/OpenCode adapter。项目级 Codex、admin、Codex plugin 与 OpenCode 配置来源仍由后续阶段验证和启用。Task 2 先在 spec §14.2/§14.3 写明这个范围调整。
4. **插件按整个 plugin 控制。** `skills=["x"]` 不隐式将 x 所属 plugin 加入允许列表；`plugins.claude` 才是 plugin 开关。允许 plugin 会暴露该 plugin 提供的全部能力；扫描其 skills 是身份与展示用途。仅有未允许 Claude plugin location 的选中 ID 记 unavailable，提示在 `plugins.claude` 中允许；与普通 native location 同 ID 时，普通 native 仍有效。先在 Task 2 澄清 spec §4.4/§7.4，避免摘要声称已启用实际关闭的 plugin skill。
5. **只读检查与写入分离。** 外来扫描在读取 SKILL.md 前检查文件类型，普通文件也必须有界读取，不能等 Inspector 才拒绝特殊或超大入口。dry-run 做相同的来源读取、自包含检查、限额检查和配置规划；只调用 `Session.Preview`，仍沿用 Phase 1 的旧会话回收。实际启动逐文件复制到 staging，全部成功才 Publish。
6. **Windows 探测与交接分开。** `proc` 的 Windows Job 属于本期 §9.2；最终 agent 的 Job、控制台事件、`.cmd` 垫片解析、退出后会话清理仍在 Phase 6。Windows 可用真实 `.exe`/测试 helper 验证探测和 dry-run；不得把当前 `handoff.ErrUnsupported` 改成临时 `exec.Command` 实现。
7. **冲突检查在 launch 调用。** 具体检测代码放 `internal/cli/conflict.go`，以函数注入 `launch.Service`，在合并 selection 后、inventory 前执行；业务包不向上 import cli。来源分别保留为 config/command-line。
8. **输出只改变显示。** 转义不修改 argv、继承环境、JSON 文件字节或投影内容；不在 agent 接管终端之后代理其 stdout。列表、错误、warning、collision、version 中的外部值都覆盖。
9. **投影目标采用保守的大小写冲突规则。** ID 和有效名保持原拼写；目标 basename 及 skill 内的相对路径用 `strings.EqualFold` 比较，`Foo`/`foo` 即使在大小写敏感卷上也视为冲突。选择这项明确的跨平台限制，是因为 dry-run 不创建探测文件，且不能仅凭 GOOS 推断卷或目录是否区分大小写。实际复制另用独占创建防止任何未预见的路径别名覆盖文件；本规则不声称完整模拟所有文件系统的 Unicode 等价规则。

## 任务顺序

`1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14 → 15 → 16 → 17`

- Task 1–2：真实 Claude 门禁与 spec 补充。
- Task 3–6：输出工具、测试探测程序、受限进程执行。
- Task 7–8：plugin inventory 与请求级依赖装配。
- Task 9–12：扫描来源、四状态解析、投影检查与 staging 复制。
- Task 13–15：完整 settings、编排、CLI 输出。
- Task 16–17：端到端回归、质量门禁、真实验收和交付文档。

每个开发 Task 的 Step 1–5 分别是测试、红灯、最小实现、绿灯、提交。测试矩阵中的每一行作为一个小步执行，不先写完整包再补测试。下文 Go 代码块给出新增接口或关键可执行片段；测试文件补齐 `package xxx_test` 与标准 import，辅助 fixture 放在同包测试文件中。

### Task 1: 验证 `--add-dir` 名字、开关与复制资源

**Files:**

- Modify: `docs/verification.md`（Claude 第 3 条）。
- Modify if contradicted: `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §3/§7.1/§7.5。

- [x] **Step 1: 建立一次性实验目录**

沿用 `docs/verification.md` 的隔离原则，明确设置 HOME、CLAUDE_CONFIG_DIR、SKOPE_HOME；cwd 是 fixture/repo。创建：

```text
fixture/
  home/
  claude/
  skope/
  repo/.git/
  addDir/.claude/skills/projected-check/SKILL.md
  addDir/.claude/skills/projected-check/references/marker.md
  on.json
  off.json
```

`SKILL.md` 的 name 故意写成 `different-frontmatter-name`，正文要求读取同目录 `references/marker.md` 并输出其中 marker；文件中放唯一、无秘密的测试字符串。on/off JSON 分别为：

```json
{"skillOverrides":{"projected-check":"on"}}
```

```json
{"skillOverrides":{"projected-check":"off"}}
```

- [x] **Step 2: 运行对照组**

在已设置实验环境的 shell 中运行，`$FIXTURE` 和 `$CLAUDE_EXE` 为执行者设置的绝对路径：

```sh
"$CLAUDE_EXE" --version
"$CLAUDE_EXE" --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/on.json" -p /projected-check --max-turns 2 --output-format text
"$CLAUDE_EXE" --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/off.json" -p /projected-check --max-turns 2 --output-format text
"$CLAUDE_EXE" --settings "$FIXTURE/on.json" -p /projected-check --max-turns 2 --output-format text
```

Expected：on 组可以调用 basename 名字并读取资源；off 组给出禁用证据；无 add-dir 组不可发现。不能只靠退出码或模型自然语言自述判定。额外检查 frontmatter 名是否意外成为可调用名字。

- [x] **Step 3: 记录可复现证据**

记录日期、完整版本、OS、fixture 内容、完整命令、可见名字和确定性拒绝信息。若沿用 Phase 1 的 WSL → Windows bridge，必须同时转换 `--settings` 与 `--add-dir`，并标明这不证明原生 Linux Claude 行为。provider 凭据只通过被授权的实验进程环境使用，不写进 fixture、命令记录或仓库。

- [x] **Step 4: 根据观察更新状态**

只有证据支持时标「通过」。若名字、加载或 override 语义不符，先修订对应 spec，再更新本计划的投影名字与 fixture；不要继续实施旧假设。

- [x] **Step 5: 提交证据**

```sh
git add docs/verification.md
git commit -m "docs: verify Claude add-dir skill controls"
```

若同时修订 spec，显式加入该文件。不要提交实验环境或认证文件。

### Task 2: 验证 plugin 枚举并补齐实现契约

**Files:**

- Modify: `docs/verification.md`（Claude 第 10 条）。
- Modify: `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §4.1/§4.4/§7.1/§7.4/§8.3/§11.2/§14.2/§14.3。
- Create after verification: `internal/agent/claude/testdata/plugin-list.valid.json`、`plugin-list.suppressed.json`（后者是未信任快照中的精确占位子集）。
- Create after verification: `internal/agent/claude/testdata/plugin-marketplaces.valid.json`、`plugin-marketplace-directory.valid.json`（directory marketplace 原地加载需要这些元数据；无需额外读取 installed_plugins.json）。

- [x] **Step 1: 制作不依赖公网的本地 marketplace**

在临时目录放 `.claude-plugin/marketplace.json`，定义 `allowed-plugin`、`blocked-plugin`，各自包含 `.claude-plugin/plugin.json` 和 `skills/check/SKILL.md`；marker 不同。用真实 CLI 的 help 核对当前版本的 marketplace add、plugin install 和 scope 选项，然后在隔离的 CLAUDE_CONFIG_DIR 与 repo 下安装。记录最终执行的完整命令，不把推测的安装 schema 固化为事实。

- [x] **Step 2: 采集一次枚举与布局证据**

```sh
"$CLAUDE_EXE" plugin list --help
"$CLAUDE_EXE" plugin list --json
```

确认以下事实并记录：

- 顶层是数组还是对象；每项 id/enabled/scope 的类型、scope 实际枚举、缺失字段情况。
- user/project/local 中同 ID 多次安装的表示；当前 cwd 是否改变有效安装项。
- 关闭的安装项仍会列出；仅 marketplace 可用但未安装的项不进入已安装集合。
- 可否直接取得有效 install path；否则读取哪份安装元数据、哪一版 schema、如何关联当前 projectPath 和安装路径。不得遍历 cache 后按版本字符串排序猜「最新版本」。
- plugin skill 的实际有效名是否含 plugin namespace；`skills/<basename>` 仍作为 ID，namespace 只进入 `Location.Names`。
- 验证列表中存在但三层 settings 均无对应键的已安装 plugin；确认它仍必须进入关闭全集。
- 当前官方文档还描述了 skills 目录内含 plugin manifest 的自动 plugin。检查当前验证版本是否支持、是否被 plugin list 枚举；若支持，明确其位置来源，不能只扫描 cache 而漏掉已安装集合。若不支持，记录版本边界。

- [x] **Step 3: 验证 plugin/bundled 控制与选择语义**

临时 `--settings` 写 `enabledPlugins` 两项 true/false；验证允许组和禁止组。再验证 `skillOverrides` 无法替代 plugin 开关。`disableBundledSkills` 的 true/false 各做对照，记录当前版本内置 skill 的可见性证据。

将测试数据缩减为无用户信息的 fixture；绝对路径换成说明明确的测试根，测试加载时替换。插件列表 fixture 必须来自实际结构，新增未知非关键字段应被解析器忽略。

- [x] **Step 4: 先修订 spec 中的六处接法**

1. §4.1 plugin 行补枚举依据、安装路径来源和 namespace；不能保留「待验证」后直接实现。
2. §4.4/§7.4 明确 plugin ID 选择不隐式启用 plugin；关闭 plugin 中的选中 skill 如何报告 unavailable。
3. §7.1 说明 `claude.Options` 由启动工厂传入；`Inventory.PluginIDs` 是已安装 ∪ settings 已有键，missing 判断必须在丢弃「已安装」集合前完成。
4. §14.2/§14.3 将本计划所述两条全局外来扫描行前移；后续 phase 保留其余扫描与 adapter 行为。
5. §11.2 允许 `proc` 和 `host` 的普通文件打开/路径检查使用平台文件。原文「平台差异只出现在 handoff/session」与 §9.2 的进程树要求、读取前拒绝特殊入口的实现需求冲突；不得为迁就旧句子在通用文件中混写 GOOS 分支。
6. §4.1/§4.4/§8.3 补外来扫描和投影边界：SKILL.md 的类型检查、有界读取在 Inspector 前完成；特殊或超过单 skill 字节限额的外来入口保留 ID 和结构化拒绝原因，不能伪造 frontmatter 名或记 missing。目标路径采用本文的保守大小写冲突规则，按 selection 顺序保留首个可用候选，后续冲突 ID unavailable；同一 skill 内含大小写别名的文件/目录也拒绝。投影写入独占创建，不能沿用 O_TRUNC 覆盖已有文件。链接环、非普通文件及会改变加载语义的 plugin 目录不可投影。明确 DiscoveryPath/RealPath 指向 SKILL.md，复制根由入口目录解析；Inspect 后源内容改变时失败并清理 staging。真实读取/写入错误仍按 §10 fail-closed。

这些是执行前的设计补充，不把它们标成原 spec 已有内容。六个冻结类型结构与 Adapter 方法签名保持不变。

- [x] **Step 5: 提交门禁和契约**

```sh
git add docs/verification.md docs/superpowers/specs/2026-09-02-skill-scope-design.md internal/agent/claude/testdata
git commit -m "docs: establish the Phase 2 Claude integration contract"
```

Expected：第 3、10 条均有结论；若还未完成，后续任务保持未勾选。文档查询可以支持实验设计，不能代替真实验证。

实际结论：list 顶层数组；同 ID 多 scope 保留原序；缓存位置取首个适用安装记录，directory marketplace 必须通过 known_marketplaces/catalog 解析原地根；自动 `@skills-dir` 在 list 中可见，未信任项目的精确 suppressed 占位证明清单不完整，按类型化 inventory 错误阻断 active dry-run/launch。fixture 路径以 `/fixture` 为测试根，测试按当前平台临时根替换。完整矩阵见 `docs/verification.md` 第 10 条。

### Task 3: `termsafe` 转义、环境值脱敏与 stderr 摘要

**Files:**

- Create: `internal/termsafe/escape.go`、`internal/termsafe/escape_test.go`。
- Create: `internal/termsafe/redact.go`、`internal/termsafe/redact_test.go`。

- [x] **Step 1: 写黑盒测试**

```go
func TestEscapePreservesTextAndEscapesTerminalControls(t *testing.T) {
	cases := []struct{ in, want string }{
		{"中文 path", "中文 path"},
		{"a\n\tb", `a\x0a\x09b`},
		{"\x1b]0;title\a", `\x1b]0;title\x07`},
		{"\u009b31m\u202e", `\x9b31m\u202e`},
		{"\u061c\u200e\u200f\u2028\u2029", `\u061c\u200e\u200f\u2028\u2029`},
	}
	for _, tc := range cases {
		if got := termsafe.Escape(tc.in); got != tc.want {
			t.Errorf("Escape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

另测所有 C0、DEL、C1、U+202A–U+202E、U+2066–U+2069；普通反斜杠保持原样；非法 UTF-8 不得变成原始控制字节。脱敏键大小写不敏感，七个词逐个覆盖；`monkey` 也含 key，按 spec 脱敏。

- [x] **Step 2: 运行红灯**

Run: `go test ./internal/termsafe -v`

Expected：新增包/函数尚不存在导致 FAIL。

- [x] **Step 3: 实现纯函数**

```go
func Escape(value string) string
func EnvValue(key, value string) string
func StderrExcerpt(raw []byte) string
```

- `Escape` 用 rune 单趟遍历，受限字符 ≤ 0xff 写 `\\x%02x`，其余写 `\\u%04x`；不把中文全转 Unicode escape。
- `EnvValue` 先按 key 判定 `<redacted>`，否则 Escape(value)。`OPENCODE_CONFIG_CONTENT` 的整体值永远不从此函数展示，CLI 只展示专门的注入字段；本期 Plan.Env 为空。
- `StderrExcerpt` 先截断原始输入至 2048 bytes（必要时退到完整 UTF-8 边界），再 Escape；被截断时加静态标记。不要先扩展转义后按任意字节切碎结果。

- [x] **Step 4: 运行绿灯**

Run: `go test ./internal/termsafe -v`

Expected：PASS；2048/2049 bytes、空输入、边界多字节字符有断言。

- [x] **Step 5: 提交**

```sh
git add internal/termsafe
git commit -m "feat: escape terminal text and redact environment values"
```

执行记录（2026-09-05）：按 Escape → EnvValue → StderrExcerpt 分三轮先写测试，分别运行 `go test ./internal/termsafe -v` 得到缺少非测试 Go 文件、未定义 EnvValue、未定义 StderrExcerpt 的预期红灯；各轮最小实现后同命令均 PASS。最终 `go test -short ./...` PASS；`gofmt`、`goimports` 与 `git diff --check` 通过。摘要采用固定 ` [truncated]` 标记，只读取原始前缀并线性扫描完整 UTF-8 边界。

### Task 4: fake agent 的 plugin 探测模式

**Files:**

- Modify: `internal/testutil/fakeagent/main.go`。
- Modify: `internal/testutil/build_test.go`。
- Create: `internal/testutil/probe_test.go`。

- [x] **Step 1: 写调用真实 helper 二进制的失败测试**

`testing.Short()` 时跳过构建测试；使用 `testutil.BuildFakeAgent(t)`，设置独立输出路径。覆盖：

| 调用 | 应有行为 |
|---|---|
| 精确 argv `plugin list --json` | stdout 默认合法空安装列表；不要求 FAKEAGENT_OUT |
| 设置 `FAKEAGENT_PLUGIN_JSON` | 原样 stdout，便于非法 JSON/字段错误测试 |
| 设置 `FAKEAGENT_PLUGIN_EXIT=7` | 探测退出 7，最终启动的 FAKEAGENT_EXIT 不参与 |
| 设置 `FAKEAGENT_PROBE_LOG=<file>` | 追加一次 argv/env/cwd JSON record，使用独立日志 |
| 普通 argv | 保留原 FAKEAGENT_OUT/FAKEAGENT_EXIT 契约 |

空列表默认形状已按 Task 2 的真实验证结果确定为 `[]`。

- [x] **Step 2: 运行红灯**

Run: `go test ./internal/testutil -run 'TestFakeAgentPlugin|TestBuildFakeAgent' -v`

Expected：plugin list 被旧 run 当作启动记录，测试 FAIL。

- [x] **Step 3: 在原 run 的开头分流**

```go
func isPluginList(args []string) bool {
	return len(args) == 3 && args[0] == "plugin" && args[1] == "list" && args[2] == "--json"
}
```

只有精确匹配才调用独立的 `runPluginList`，普通 run 保持原意。探测日志不能包含仓库外真实用户环境；测试进程传临时 home 和测试变量，敏感值不输出到失败日志。

- [x] **Step 4: 运行绿灯**

Run: `go test ./internal/testutil -v`

Expected：旧构建测试与新增探测测试 PASS，普通退出码 23 不影响探测。

- [x] **Step 5: 提交**

```sh
git add internal/testutil
git commit -m "test: support Claude plugin probes in the fake agent"
```

实施记录（2026-09-05）：先新增真实 helper 测试，确认旧实现以 99/23 退出导致红灯；实现精确分流、原样输出、独立退出码和追加 JSONL 后，`go test ./internal/testutil -v` 与 `go test -short ./...` 通过。补充显式空 JSON、无效探测退出码、参数缺失/多余/近似匹配、连续探测不覆盖普通启动记录；测试仅传临时 HOME/USERPROFILE/CLAUDE_CONFIG_DIR 与必要系统变量，失败信息不输出完整环境。

### Task 5: `proc` 的通用限额与 Unix 进程树终止

**Files:**

- Create: `internal/proc/proc.go`、`internal/proc/proc_test.go`。
- Create: `internal/proc/process_unix.go`、`internal/proc/process_unix_test.go`。
- Create: `internal/proc/process_windows.go`（本 Task 只提供可构建的明确 unsupported；Task 6 完成）。

- [x] **Step 1: 写受控子进程测试**

用当前测试二进制的 `TestProcHelperProcess` 作为 helper，参数明确限定 helper 分支，避免递归跑全部测试。测试包括：成功输出、非零退出、stdin 立即 EOF、继承指定 cwd/env、父 context 已取消、timeout、stdout 超限、stderr 超限、子进程生成孙进程持续持有 pipe。

固定默认值为 15 s 与每路 4 MiB；测试使用更短的注入上限，并额外覆盖默认常量。恰好达到上限成功，多一个 byte 失败；两路分别 3 MiB 成功，不能把它们错误合并为 4 MiB。

- [x] **Step 2: 运行红灯**

Run: `go test ./internal/proc -run 'TestRunner|TestLimits' -v`

Expected：FAIL。Unix 进程组行为在 Linux/macOS 执行；Windows 暂只跑通用边界测试。

- [x] **Step 3: 实现 Runner 与平台执行入口**

```go
const DefaultTimeout = 15 * time.Second
const DefaultOutputLimit int64 = 4 << 20

type Request struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

type Runner struct {
	Timeout     time.Duration // zero uses DefaultTimeout
	OutputLimit int64         // zero uses DefaultOutputLimit
}

func (r Runner) Run(ctx context.Context, req Request) (Result, error)
```

`TimeoutError`、`OutputLimitError`、`ExitError` 暴露可判定字段；error 不附加 stdout，不打印完整 argv/env。stderr 只保留需展示的原始前缀，CLI 最终统一做转义。设置负 timeout/limit 报配置错误；调用方不能注入 nil Env 后默默读取宿主环境。

Unix 实现：

1. `exec.Command` 使用已解析 executable、`Dir`、复制后的 `Env`；stdin 接立即 EOF 的输入，stdout/stderr 独立有界接收器。
2. `SysProcAttr.Setpgid=true`；启动后保存进程组 ID。任何输出超过上限立即触发取消，不能只返回 writer 错误后等待无限运行的子进程。
3. 以父 context 与 15 s 中较早者为期限；超时/取消/超限用负 PGID 向组发送 SIGKILL，处理已退出的 ESRCH。
4. 每个成功 Start 必须且只能 Wait 一次；收齐读取 goroutine。直接父进程退出而孙进程持有 pipe 也受同一期限约束。
5. 正常辅助命令结束后也清理剩余后代，不允许它成为后台服务；归还有界输出与明确错误。

不使用临时文件：输出限制已经允许内存缓冲，因此 §9.2 临时文件契约无本期实例。以后若引入临时文件，必须同时实现随机名、0600 和全路径清理。

- [x] **Step 4: 回归进程树和错误边界**

Run: `go test ./internal/proc -v`

Expected：PASS；用 helper 的 PID/结束同步信号验证孙进程结束，不能仅断言 Run 很快返回；避免仅靠固定 sleep 制造时序。

- [x] **Step 5: 提交**

```sh
git add internal/proc
git commit -m "feat: bound auxiliary processes and terminate Unix process groups"
```

实施记录（2026-09-05）：先新增黑盒 helper 测试，首次 `go test ./internal/proc -timeout 60s` 因尚无生产实现而失败。实现后，Windows `go test -short ./...` 通过；Linux/WSL Go 1.27.1 的完整 `proc` 测试与 `go test -race ./internal/proc -timeout 90s` 通过。默认 15 s、每路 4 MiB，错误 stderr 原始前缀最多 2 KiB；验证两路各 3 MiB、精确上限/多一个 byte、stdin EOF、显式空环境、提前取消、超时、退出码与敏感输出边界。后代测试使用 TCP 就绪与连接 EOF 同步，覆盖取消、超时、输出超限、父进程先退出持 pipe、正常父进程退出留下无 pipe 后代。gofmt/goimports 与局部 golangci-lint（0 issues）完成。通用层收齐一次 Wait 与两个读取结果，Unix 用独立进程组清理；Windows 本任务只提供明确 `ErrUnsupported`，执行行为测试保留到 Task 6，未更改 handoff。

### Task 6: Windows 辅助子进程的独立 Job

**Files:**

- Modify: `internal/proc/process_windows.go`。
- Create: `internal/proc/process_windows_test.go`、`internal/proc/process_tree_test.go`；调整 `proc_test.go` / `process_unix_test.go`，共用后代行为测试。
- Verify only: `internal/handoff/handoff_windows.go`（保持 ErrUnsupported）。

- [x] **Step 1: 写 Windows 行为测试**

同 Task 5 的 helper 协议，增加：子进程及孙进程超时后退出、输出超限终止树、父进程先退出而孙进程持有输出管道、带空格/引号/空字符串参数、环境变量值包含 `=`、取消时所有句柄归还。Job 创建或赋值失败必须不启动可执行的 probe。

- [x] **Step 2: 在 Windows 运行红灯**

Run: `go test ./internal/proc -run 'TestRunner|TestWindows' -v`

Expected：上一 Task 的 unsupported 导致 FAIL。

- [x] **Step 3: 实现 Windows 平台后端**

使用固定版本 `golang.org/x/sys/windows` 的本地源码核对调用签名，补充需要的官方文档查询后实现：

- 创建独立 Job，设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，不允许 breakaway；不要把 skope 本身加入这个短命 Job。
- 用 `STARTUPINFOEX` 的 `PROC_THREAD_ATTRIBUTE_JOB_LIST` 在创建进程时绑定 Job，避免先运行再 Assign 的逃逸窗口。该方式由 Microsoft 官方文档说明；若目标 Windows 不支持或外层 Job 不允许，返回类型化错误，probe 不降级为无 Job。
- 以 `CreateProcess` 执行精确 application path，使用正确的 Windows 参数引用和 UTF-16 环境块；只继承 stdin/stdout/stderr 句柄。stdin 使用 EOF/NUL，独立读取 stdout/stderr 并复用通用限额。
- helper 不弹新窗口。进程、主线程、Job、pipe、attribute list 在成功和每个失败分支都关闭。超时/取消/超限终止 Job 并等待输出读取完成。
- 不实现 npm `.cmd` 解析。若解析到 `.cmd`，给出当前阶段需要真实可执行文件的错误；Phase 6 再提供与 handoff 共用的真实目标解析。

参考：[Microsoft 的创建时 Job 绑定说明](https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812)、[Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)。这里只落实 §9.2，不复制 §8.4 的最终交接流程。

- [x] **Step 4: 运行绿灯**

Run: `go test ./internal/proc -v`

Expected：Windows helper 树终止测试 PASS；Linux/macOS 后续 CI 继续执行 Unix 实现。

- [x] **Step 5: 提交**

```sh
git add internal/proc
git commit -m "feat: contain Windows auxiliary probes in a job"
```

实施记录（2026-09-05）：解除 Windows skip 后，共用正常运行与 Windows 参数、环境值、脚本拒绝测试先因 ErrUnsupported 失败。实现独立 Job 与创建时 JOB_LIST 绑定；仅复制三个标准句柄，UTF-16 显式环境块、NUL stdin、CREATE_NO_WINDOW。Windows `go test ./internal/proc -v -timeout 90s`、`go test -short ./...`、局部 golangci-lint（0 issues）通过；WSL `go test -race ./internal/proc -timeout 90s` 通过，gofmt/goimports 完成。实测覆盖后代取消/超时/超限、父先退出持有或不持有 pipe、20 轮正常/超时/取消/创建失败句柄归还、空/含引号参数和空格 exe 路径、空环境及含等号/中文值。隔离 helper 进入 active-process-limit=1 外层 Job 后，Runner 返回 ContainmentError 且 probe marker 未创建。Windows TCP 后代测试显式传 SystemRoot 供 Winsock 初始化，以 EOF/WSAECONNRESET 确认连接结束；生产不补环境变量。当前 Windows 已验证创建时绑定；旧系统或约束不允许时返回类型化错误，不作无 Job 降级。handoff_windows.go 保持 ErrUnsupported。

### Task 7: Claude plugin JSON、三层 settings 与完整 inventory

**Files:**

- Modify: `internal/agent/claude/adapter.go`、`internal/agent/claude/inventory.go`、`internal/agent/claude/inventory_test.go`。
- Create: `internal/agent/claude/plugins.go`、`internal/agent/claude/plugins_test.go`。
- Create: `internal/agent/claude/plugin_scan.go`、`internal/agent/claude/plugin_scan_test.go`。
- Modify: `internal/agent/claude/testdata/plugin-list.valid.json`（只按验证证据调整）。
- Modify: `internal/skill/scan.go`、`internal/skill/scope.go`、`internal/skill/scan_test.go`（显式 roots API）。

- [ ] **Step 1: 写输入校验与集合语义测试**

用 fake Runner 记录请求；合法 fixture 取 Task 2。每次有效 Inventory 恰好调用一次 plugin list。测试至少覆盖：

| 输入 | 结果 |
|---|---|
| 三层各自具有不同 skillOverrides/enabledPlugins 键 | 两种集合分别去重、排序 |
| settings 中 plugin 值为 false | 键仍进入全集 |
| 已安装 plugin 未出现在 settings | 仍进入 PluginIDs |
| Options 允许未安装 plugin，settings 中已有同名 true | missing 告警，不能误当已安装 |
| 允许项既未安装也不在 settings | missing 告警；Plan 后续仍写 true |
| 空安装列表 | 合法；settings 键仍保留 |
| 缺 id/enabled/scope、enabled 为 null/字符串、无效两段 ID | fail-closed，含项索引和字段名，不打印整段 JSON |
| 同 ID 不同合法 scope | 集合去重，安装入口按 Task 2 的规则解析 |
| cache 同 ID user/project/local 不同路径，调整记录顺序 | 保留原序，首个 user 或 projectPath 包含 cwd 的项生效；nested cwd 匹配、相似字符串前缀不匹配 |
| 只有其他项目安装，无适用 cache 行 | ID 仍进入 installed/PluginIDs，不建 native location，不误报 missing、不令全部启动失败 |
| directory marketplace cache 有旧内容、源目录新增 skill | known_marketplaces + catalog 定位原地根，枚举新增项 |
| personal/project 自动 plugin | 使用 list 原地路径；project 自动项允许缺 projectPath；普通 scanner 遇 manifest 目录停止 |
| 精确 suppressed 占位 | 类型化 inventory 错误，提示先在 Claude 独立完成当前 workspace 信任再重试；不返回部分 inventory，不回显 notes，其他空 installPath 仍失败 |
| JSON 根错误、trailing value、语法错误 | fail-closed |
| probe 退出失败、超时、超限 | 不继续扫描 plugin 目录，不返回部分 inventory |
| `enabledPlugins` 为 null/数组，值不是 bool | settings 文件路径 + 字段错误 |
| 无关 settings 字段 | 接受；不重新序列化或泄漏它们 |

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/agent/claude ./internal/skill -run 'TestInventory|TestPlugin|TestScannerExplicit' -v`

Expected：新行为 FAIL。

- [ ] **Step 3: 增加非冻结类型与显式根扫描**

```go
// internal/agent/claude/adapter.go
type ProbeRunner interface {
	Run(context.Context, proc.Request) (proc.Result, error)
}

type Options struct {
	Executable string
	Plugins    []string
	Bundled    bool
}

// New 深复制 Options.Plugins；Options 保存在 Adapter 私有字段中。
func New(scanner SkillScanner, fsys ReadFileFS, runner ProbeRunner, opts Options) Adapter
```

扩大消费方 `SkillScanner`，在现有 `ScanClaude` 之外增加 `ScanRoots([]skill.Root) (skill.ScanResult, error)`。把已有私有 `scanRoot` 提升为下列类型；这不是冻结的 Location：

```go
type Root struct {
	Path        string
	Kind        Kind
	Level       Level
	Source      Source
	Scope       string
	VisibleTo   []Agent
	PluginID    string
	PluginAgent Agent
	NamePrefix  string // verified plugin namespace, empty for ordinary skills
}
```

逐个更新原 roots 的 VisibleTo，command 仍只有 Claude；名字构建遵循来源与验证后的 plugin namespace，不把 `Names[Claude]` 写死在底层 scanner。同步现有 fakeScanner 和 Inventory fixture 注入合法空 probe，避免旧测试因缺依赖而失去原行为覆盖。

`plugins.go` 内部保留安装项类型：id/enabled/scope 必须能够区分缺失与零值，特别是 `enabled:false` 与缺字段。scope 枚举和安装路径来源以 Task 2 fixture 为准；未知非关键字段兼容忽略，未知关键来源不能当作空列表成功。

为精确 suppressed 占位定义可由 `errors.As` 识别的 `IncompletePluginInventoryError`；其静态错误说明指向在 Claude 中完成当前 workspace 信任后重试，不回显子进程 notes。这类响应表明已知预存 plugin 未被枚举，不能只告警后继续 handoff，否则随后接受信任可能加载未写 false 的 plugin。skope 不写 Claude 信任配置；none 不调用 probe，仍完整旁路。

按 spec §4.1 已验证规则解析：真实列表为数组；普通 project/local 记录要求 projectPath，自动 @skills-dir project 项不要求。缓存记录原序首个适用项决定根，不能按 scope 或版本排序；所有真实 ID 先进入 installed 集合，无适用项只是不建 location。读取 known_marketplaces 对应来源：directory 的相对字符串 catalog source 决定原地根，已知缓存来源使用 list.installPath，自动 plugin 直接用 list 路径。无关 catalog 项不进入 installed。不得通过模型调用、遍历旧版本目录或猜默认 cache 路径兜底。manifest 的 name 决定 NamePrefix，SKILL.md frontmatter 不改变 plugin namespace。

Inventory 顺序为 Claude 原生扫描 → 三层 settings → plugin list → 安装目录定位/扫描 → Build 合并。probe Request 固定为：

```go
proc.Request{
	Executable: a.options.Executable,
	Args:       []string{"plugin", "list", "--json"},
	Dir:        env.Cwd(),
	Env:        env.Environ(),
}
```

不附加 config.args、用户参数或 skope 控制参数；不能硬编码 PATH 中另一个 `claude`。已安装集合只用来判定 missing 与枚举入口；`Inventory.PluginIDs = installed ∪ settings keys`，Plan 再并入允许项。

plugin location 填 `LevelPlugin/SourceClaude/PluginID/PluginAgent`，ID 仍按 basename；名字按已验证 namespace。多 location 合并后重新 `skill.Build` 生成全部碰撞，不简单拼接两份碰撞列表。只扫验证确定的有效安装路径；缓存历史版本不能冒充多个当前入口。

settings 不存在允许；已确定有效安装目录读失败按 §10 报错。只收集字段键，既不依赖 settings 合并优先级，也不改写任何持久化文件。

- [ ] **Step 4: 运行绿灯并验证不可变性**

Run: `go test ./internal/agent/claude ./internal/skill -v`

Expected：PASS；修改构造参数或返回 inventory 不能影响第二次调用；缺 runner/executable 是明确配置错误，不偷偷跳过枚举。

- [ ] **Step 5: 提交**

```sh
git add internal/agent/claude internal/skill
git commit -m "feat: enumerate Claude plugins and settings control keys"
```

### Task 8: 请求级 adapter 工厂与冲突检查

**Files:**

- Modify: `internal/launch/launch.go`、`internal/launch/launch_test.go`。
- Modify: `internal/cli/root.go`、`internal/cli/root_test.go`。
- Create: `internal/cli/conflict.go`、`internal/cli/conflict_test.go`。
- Modify: `internal/cli/integration_test.go`（共享 fixture 增加默认 probe 支持）。

- [ ] **Step 1: 写顺序、来源和隔离旁路测试**

在 launch 记录型依赖测试中断言：配置/回收/解析 executable/解析 set/加载并集 → conflict → registry factory → inventory。conflict 失败时 factory、probe、Stage、Handoff 都没有调用。

在 `package cli_test` 通过导出的 `cli.CheckConflicts` 黑盒测试 Claude 五个冲突参数：`--settings`、`--setting-sources`、`--plugin-dir`、`--plugin-url`、`--add-dir`，每个覆盖独立 token 与 `=value`。配置和用户两种来源都测；错误包含参数名与来源，不含 sentinel 秘密值。`Application.Execute` 注入返回该错误的 runner，验证命令错误传播；真正的启动前拒绝由 launch 顺序测试和 Task 16 集成测试覆盖。

补 `-s none` 完整旁路、普通 `--model` 允许、`--settings-file` 不误判、`--` 透传后出现冲突仍拒绝。检测不通过连接字符串寻找子串，不改变传入 slice。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/launch ./internal/cli -run 'TestServiceRun|TestClaudeConflict' -v`

Expected：FAIL。

- [ ] **Step 3: 替换 Service.Registry 为受控工厂**

```go
type RegistryFactory func(executable string, selection config.Selection) (AdapterRegistry, error)
type ConflictChecker func(agent skill.Agent, configArgs, userArgs []string) error
```

`Service` 增加 `NewRegistry RegistryFactory` 与 `CheckConflicts ConflictChecker`，移除固定 Registry 字段并更新现有 fakeRegistry 装配；保留现有 `AdapterRegistry.Get` 与 `agent.NewRegistry`。工厂由 CLI 提供，内部：

```go
adapter := claude.New(scanner, fsys, proc.Runner{}, claude.Options{
	Executable: executable,
	Plugins:    append([]string(nil), selection.Plugins["claude"]...),
	Bundled:    selection.Bundled,
})
registry, err := agent.NewRegistry(adapter)
```

工厂入参深复制 `selection.Skills` 和 `selection.Plugins`，防止依赖修改解析结果。active 路径缺 factory/checker 返回内部配置错误；none/help/version/list 不需要它们。工厂不重读配置、不重做 LookPath。

冲突检测核心：

```go
func claudeConflict(args []string) string {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--settings", "--setting-sources", "--plugin-dir", "--plugin-url", "--add-dir":
			return name
		}
	}
	return ""
}
```

导出消费入口 `func CheckConflicts(agent skill.Agent, configArgs, userArgs []string) error`，分别扫描 configArgs/userArgs 后构造只带 Flag/Source 的 `ConflictError`。CLI 包仍是 internal 包，此函数只供生产装配和黑盒测试使用；不得由 launch import 它。非 Claude 暂无本期检查，未来 adapter 实施时增加对应分支。不解析或打印值；none 路径在调用前返回。将原 `args.go` 首个未知 token 停止解析规则完整保留。

- [ ] **Step 4: 回归**

Run: `go test ./internal/launch ./internal/cli -v`

Expected：PASS；fake probe 使用配置的 fake executable，启动日志仍保留配置参数与用户参数顺序。旧 integration 的 settings 断言暂保留旧输出，Task 13 再同步三个新字段。

- [ ] **Step 5: 提交**

```sh
git add internal/launch internal/cli
git commit -m "feat: bind Claude launch options and reject conflicting flags"
```

### Task 9: 外来全局来源扫描与完整 skill 集合

**Files:**

- Create: `internal/skill/foreign.go`、`internal/skill/foreign_test.go`。
- Modify: `internal/skill/scan.go`、`internal/skill/scope.go`。
- Modify: `internal/skill/resolve.go`（本 Task 先定义 special-file/limit-exceeded 两个原因常量）。
- Create: `internal/host/regular.go`、`internal/host/regular_test.go`。
- Create: `internal/host/regular_unix.go`、`internal/host/regular_windows.go`。
- Create: `internal/launch/inventory.go`、`internal/launch/inventory_test.go`。
- Modify: `internal/launch/launch.go`。
- Modify: `internal/cli/root.go`。

- [ ] **Step 1: 写数据驱动扫描测试**

通过现有 `mapFileSystem`/`fstest.MapFS` 包装器测试：

| root | Source | Names |
|---|---|---|
| `<home>/.agents/skills` | agents | Codex/OpenCode = frontmatter name，缺失回退 basename；没有 Claude |
| `<CODEX_HOME>/skills` | codex | 只有 Codex 名字 |
| CODEX_HOME 未设置或 trim 后为空 | codex | 默认 `<home>/.codex/skills` |

额外覆盖：目录缺失、进入后权限错误、恶劣 frontmatter、入口 link、相同 RealPath 的两个发现入口保留、全局 Claude 与 foreign 同 ID 合并。当前不扫描项目 `.agents/.codex` 或 Codex plugin。

增加外来 SKILL.md 的读取边界测试，必须直接经过 `ScanForeignGlobals`，不能只调用后续 Inspector：

| 入口 | 扫描结果与读取断言 |
|---|---|
| FIFO/socket/设备等特殊文件 | 不对其执行阻塞读取；保留 location 并返回 special-file 拒绝原因 |
| symlink 指向特殊文件 | 先检查最终目标类型，同样拒绝；不能在检查前打开 pipe |
| 普通文件大小恰为传入 limit | 允许完整解析；读取量最多 limit+1 bytes |
| Stat.Size 超过 limit | 不读取正文，记录 limit-exceeded |
| Stat.Size 小于限额、实际读取超过 limit | 最多读取 limit+1 bytes 后记录 limit-exceeded，不能无限 ReadAll |
| 打开/读取/关闭失败、断链、合法大小但 frontmatter 非法 | 保留路径并返回 error；不能当作可继续的 Rejection |

用记录读取量的 fake reader 证明上界，用抛错的旧 ReadFile 替身证明 foreign 分支没有调用无界入口。Unix FIFO 回归通过受测试超时约束的 helper 子进程运行，避免实现有缺陷时挂住整个测试套件；Windows 跑同一拒绝分支的假文件系统测试。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/skill ./internal/host -run 'TestScanForeign|TestScannerExplicit|TestOpenRegular' -v`

Expected：FAIL。

- [ ] **Step 3: 实现并注入第二扫描入口**

```go
func (s Scanner) ScanForeignGlobals(env host.Env, maxBytes int64) (ScanResult, error)
```

复用 Task 7 的显式 roots、名字构造与 `parseFrontmatterName`，但替换 foreign 分支的文件读取入口。当前 `scanSkillRoot` 在类型检查前调用 `FS.ReadFile`，生产实现是 `os.ReadFile`；原调用顺序不能直接复用。扫描范围仍是前置决策中的两行。

在 skill 消费方定义 `RegularFileOpener`，给 Scanner 增加 `RegularFiles RegularFileOpener`；只有 foreign 扫描要求该依赖，原生扫描测试无需为了这个字段全部改写。host.OSFileSystem 提供生产实现：

```go
type RegularFileOpener interface {
	OpenRegular(name string) (fs.File, error)
}

type ScanRejection struct {
	Source        Source
	DiscoveryPath string
	Reason        ResolutionReason
}
```

`OpenRegular` 返回只读普通文件或类型化 `host.ErrNotRegular`，不能先用普通阻塞 Open 打开 FIFO 再检查句柄。Unix 后端先检查目标类型，打开阶段使用非阻塞标志，再复核打开句柄的类型；Windows 后端在读取前拒绝设备、pipe 等非普通入口并复核句柄。链接目标检查仍保留现有断链/I/O 错误规则。所有成功打开的句柄必须关闭，关闭错误不得丢弃。

`maxBytes` 必须为正，Task 14 从 `projection.MaxBytes` 传入，skill 不反向 import projection。foreign 扫描先 Lstat/Stat 检查类型和大小，再通过 OpenRegular 获取句柄、复核类型/大小，用 `io.LimitReader(file, maxBytes+1)` 有界读取并检查实际长度。只有读取成功且未超限的内容才送进现有 frontmatter parser；测试可传小 limit，不必每次分配 20 MiB。

在未冻结的 `ScanResult` 增加 `Rejections []ScanRejection`。特殊/超限项仍建立带 Kind/Source/Level/DiscoveryPath 的 Location，由 `skill.Build` 按路径计算 ID；由于没有解析正文，Names 留空，不假造 frontmatter 回退名。拒绝原因按 `(Source, DiscoveryPath)` 随本次扫描单独传递，不给冻结的 Skill/Location/Inventory 增字段。缺失 SKILL.md 仍按既有规则跳过；真实 I/O/解析错误仍 fail-closed。

launch 消费方定义：

```go
type ForeignScanner interface {
	ScanForeignGlobals(host.Env, int64) (skill.ScanResult, error)
}
```

`Service.Foreign` 由 CLI 注入 `skill.Scanner{FS: fsys, RegularFiles: fsys}`。在 `internal/launch/inventory.go` 实现并测试合并 helper：将两份 inventory 的 locations 展平后 `skill.Build`，保留 adapter 的 SkillNames、PluginIDs、Warnings，更新 Skills/Collisions 为并集的新副本；另返回深复制的 foreign Rejections 给后续解析。通过混合 fixture 证明 helper 保留同 ID 的多来源、碰撞与拒绝原因。

本 Task 只建立扫描入口和合并 helper；Task 14 才将它接入 Service.Run，届时四状态解析与 Copier 已齐备。不能先把 foreign 集合交给旧 `ResolveNative`，造成中间提交把不可见 skill 报成 native。adapter.Inventory 本身继续遵守「该 agent 原生 inventory」注释。

- [ ] **Step 4: 回归**

Run: `go test ./internal/skill ./internal/host ./internal/launch ./internal/cli -v`

Expected：PASS；合并 helper 能返回 foreign 候选及拒绝原因，特殊文件没有被读取，超限读取不超过 limit+1；当前生产 Run 尚不调用它，Task 14 再验证 active 接入和 none 旁路。

- [ ] **Step 5: 提交**

```sh
git add internal/skill internal/host internal/launch internal/cli
git commit -m "feat: discover foreign global skills for Claude projection"
```

### Task 10: `projection` 只读清单、自包含检查与限额

**Files:**

- Create: `internal/projection/projection.go`、`internal/projection/inspect.go`、`internal/projection/inspect_test.go`。
- Create: `internal/projection/paths.go`、`internal/projection/paths_test.go`。
- Create: `internal/host/projection_fs.go`、`internal/host/projection_fs_test.go`。

- [ ] **Step 1: 写自包含检查矩阵**

```text
foreign/
  SKILL.md
  references/note.md
  scripts/helper.sh
  empty/
  links/inside -> ../references
```

至少测试：普通文件/空目录、入口本身是 link、内部文件与目录 link 指向根内、链接目标为复制根父级、兄弟目录名有相同前缀、链式 link、环、断链、FIFO/socket/设备等特殊文件。

目标路径检查还要覆盖一个 skill 内的 `references/Foo.md` 与 `references/foo.md`、`Refs/a.md` 与 `refs/b.md`、文件 `Docs` 与目录 `docs/`。按前置决策的保守规则整项 Rejection；共享同一个精确拼写的普通父目录允许。检查只比较即将创建的目标路径，不改写源文件名、不创建大小写探测文件。

`Location.DiscoveryPath` 和 `RealPath` 在当前代码中都指向 **SKILL.md 文件**。复制根从 `Dir(DiscoveryPath)` 解析，而非直接把 `RealPath` 当目录，也不能仅因 `SKILL.md` 单文件 link 到别处就把整棵复制根改到别处。

边界：2000 文件与 20 MiB 都允许；2001 或多一个 byte unavailable。按最终展开后的复制文件次数计数，不按 unique inode 去重；内部 link 导致相同内容复制两次就计算两次。目录不计入文件数，但递归栈要检测环。

`.md` 中 `../`、两种 plugin-root 变量、POSIX/Windows/UNC 绝对引用只产生 warning；普通 URL 不作为文件绝对路径。读取错误返回 error。根内 plugin manifest 按 Task 2 的契约不可投影。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/projection ./internal/host -run 'TestInspect|TestProjectionRoot' -v`

Expected：FAIL。

- [ ] **Step 3: 实现只读 Manifest**

```go
const MaxFiles = 2000
const MaxBytes int64 = 20 << 20 // also passed to ScanForeignGlobals by launch

type File struct {
	Path   string // relative destination, slash-separated
	Source string // root-relative resolved source
	Size   int64
	SHA256 [32]byte
}

type Manifest struct {
	Root        string
	Directories []string
	Files       []File
	Bytes       int64
	Warnings    []string
}

type Rejection struct {
	Reason string // stable reason code, no terminal formatting
	Path   string
}

// Root 的 Open/ReadDir 以已经打开的目录句柄为边界。
type Root interface {
	fs.ReadDirFS
	Lstat(name string) (fs.FileInfo, error)
	Resolve(name string) (relative string, contained bool, err error)
	SameFile(a, b fs.FileInfo) bool
	Close() error
}

type OpenRoot func(directory string) (Root, error)

type Inspector struct{ OpenRoot OpenRoot }

func (i Inspector) Inspect(ctx context.Context, directory string) (Manifest, *Rejection, error)
```

launch 先消费 Task 9 的扫描拒绝原因，只对通过入口检查的候选调用 Inspector，参数为 `filepath.Dir(location.DiscoveryPath)`；projection 不依赖 skill 的身份类型。递归资源中的不自包含/限额/循环/特殊文件以及目标路径冲突属于 Rejection；真实 I/O/取消属于 error。Root.Resolve 对外部目标返回 contained=false，不能先打开外部文件；断链和权限错误保留为 error。只读检查稳定排序，计算内容 hash 与实际字节数，限长读取，不能信任文件 Stat.Size 后无限 ReadAll。Manifest 不保留全文，不写临时文件，每次 Inspect 在返回前关闭所开句柄。

在 `internal/projection/paths.go` 增加纯函数 `TargetNamesConflict(left, right string) bool`，对已校验的单个路径组件使用 `strings.EqualFold`。检查 Manifest 的 Files/Directories 时逐级登记目标路径，拒绝拼写不同但组件等价的别名，以及文件/目录占用同一路径；返回 target-conflict。Task 11 复用同一比较函数检查多个 skill 的目标 basename，不能各写一份不同的归一化规则。

文件系统接口定义在 `projection` 消费方；`host.OpenProjectionRoot` 返回实现这些方法的具体类型，host 不反向 import projection。CLI 用薄闭包转换返回类型后注入 Inspector/Copier。核心 inspect/copy 文件不 import `os`；OS 实现用 `os.Root` 打开已确认的复制根，在打开内容时限制相对路径仍处于根内。解析链接用于判断拒绝原因，不能把「EvalSymlinks 检查后再用任意绝对路径 Open」当作防止链接切换的措施。需要平台 reparse 辅助时明确列入 Task 2 的 §11.2 例外，并放 host 的平台文件，禁止通用代码 scattered GOOS 分支。

入口 link 解析、内部 link 展开、根锚定失败分别有测试。按当前递归分支的文件身份检测环，不用全局 visited 错误拒绝两份合法复制。Windows 路径大小写/盘符和 macOS `/private/var` 用文件身份与平台路径处理验证，不用字符串前缀比较。

- [ ] **Step 4: 运行绿灯**

Run: `go test ./internal/projection ./internal/host -v`

Expected：PASS；检查前后目录快照不变，Manifest 没有 []byte 正文。真正 symlink/Junction 测试在有权限的平台执行，Windows 缺 symlink 权限只能跳过该 fixture，不能跳过纯路径与拒绝逻辑。

- [ ] **Step 5: 提交**

```sh
git add internal/projection internal/host
git commit -m "feat: inspect projection contents and enforce copy limits"
```

### Task 11: 四状态解析与目标名称冲突

**Files:**

- Modify: `internal/skill/resolve.go`、`internal/skill/resolve_test.go`。
- Create: `internal/launch/projection.go`、`internal/launch/projection_test.go`。
- Modify: `internal/agent/claude/settings_test.go`（AllowedNames 新语义的既有测试）。

- [ ] **Step 1: 写选择矩阵**

| 条件 | 期望 |
|---|---|
| ID 有普通目标 native 入口，也有更高/更低优先级 foreign 入口 | native，不调用 Inspector |
| ID 不在完整集合 | missing |
| 无 native、支持投影、第一个非 plugin skill 候选检查通过 | projected |
| 高优先级候选被 Rejection 拒绝、下一项通过 | 选择下一项 |
| 高优先级 foreign 候选在扫描时已记 special-file/limit-exceeded，下一项合法 | 不重新打开被拒入口，继续检查下一项 |
| 候选读取返回 I/O error | 整次失败，不靠下一候选掩盖错误 |
| 所有候选被拒绝 | unavailable，保留稳定原因 |
| 只有 foreign plugin 或不可投影 command | unavailable |
| Projection=false | unavailable，Inspector 不调用 |
| 只有 Claude plugin 且 plugin 不被允许 | unavailable，不隐式加 plugin |
| 两个被投影 ID 目标 basename 相同，或为 `Foo`/`foo` | 按同一保守规则判冲突；首个成功，后者 unavailable，dry-run 结果一致 |
| projected basename 与已发现普通 Claude native 名相同 | unavailable，避免副本被原生入口遮蔽 |

名字由目标加载方式决定：Claude 投影使用 `Base(Dir(DiscoveryPath))`，即使 ID 有 scope 或 frontmatter 名不同。不要改写冻结 Location.Names，projected 的目标名字放 `Resolution.Names`。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/skill ./internal/launch -run 'TestResolve|TestPrepareProjection' -v`

Expected：当前 `ResolveNative` 在无目标名时返回 native，测试 FAIL。

- [ ] **Step 3: 增加解析入口与 Inspector 桥接**

```go
// skill 包仅知道策略与回调，不 import projection、agent 或 config。
type ResolveOptions struct {
	Projection     bool
	AllowedPlugins []string
}

type ProjectionCheck func(Location) (ResolutionReason, error)

func Resolve(target Agent, selected []string, inventory []Skill, opts ResolveOptions, check ProjectionCheck) (Resolved, error)
```

遍历 selection 首次出现顺序，候选使用 `skill.Build` 已给出的全序；完整 native 检查先于任何 projection 检查。原因使用具名 `ResolutionReason` 常量，包括 projection-unsupported/plugin-disabled/plugin-only/command-only/outside-root/limit-exceeded/target-conflict。

launch 的 callback 先用 `(Source, DiscoveryPath)` 查询 Task 9 的 ScanRejection map，命中就返回原拒绝原因，不调用 Inspector；成功入口才调用 Inspector 并保存 Manifest。使用两个字段组成的 struct key，避免直接连接字符串造成歧义；这些 map 只在本次调用存活。

登记投影目标前调用 `projection.TargetNamesConflict`，与此前成功登记的 basename 比较；不能仅用 Go map 的原始字符串 key，也不能根据 GOOS 假设卷是否区分大小写。候选所有检查通过后才占用目标名，被拒候选不占用；冲突时继续尝试该 ID 的下一合法 location，全部失败才 unavailable。两个外来来源分别提供 `Foo`、`foo` 的测试必须断言只得到一个 projected、一个 target-conflict，原始 ID/Names 不改写。

`AllowedNames` 同时收 `StateNative` 和 `StateProjected`，保持去重排序。Task 14 移除生产中的 `ResolveNative` 调用；本 Task 先保留原生产入口，避免在 Copier 接入前产生 projected 设置。新 Resolve 的测试必须证明无目标名字不会被标成 native。因 AllowedNames 改变而受影响的旧 Claude Plan 测试同步更新其允许名预期，完整字段与 add-dir 断言仍在 Task 13 增加。

- [ ] **Step 4: 运行绿灯**

Run: `go test ./internal/skill ./internal/launch -v`

Expected：PASS；Resolution.Location 深复制，用户修改原 inventory 不能改变解析结果；每个 projected ID 恰好关联一个 Manifest。

- [ ] **Step 5: 提交**

```sh
git add internal/skill internal/launch
git add internal/agent/claude/settings_test.go
git commit -m "feat: resolve native projected unavailable and missing skills"
```

### Task 12: 逐文件复制到私有 staging

**Files:**

- Create: `internal/projection/copy.go`、`internal/projection/copy_test.go`。
- Modify: `internal/session/session.go`、`internal/session/session_test.go`。
- Modify: `internal/launch/projection.go`、`internal/launch/projection_test.go`。
- Modify: `internal/launch/launch.go`、`internal/launch/launch_test.go`（SessionManager 新增独占写入方法及 fake 实现）。

- [ ] **Step 1: 写复制与失败清理测试**

- 完整复制 SKILL.md、references、scripts、二进制内容与空目录；内部 links 展开后目标没有链接。
- 输出权限为文件 0600/目录 0700，脚本也遵守 spec，不偷偷保留 executable bit。
- 源端在 Inspect 后增删改文件、改变 link 指向或增大内容，实际启动报错并 Abort，不能复制未经检查的新内容。
- 第 N 个文件读取/写入失败，原错误保留；Abort 失败经 `errors.Join` 保留第二错误。
- 写入的最终路径越界、前缀相似、目的目录中出现 link，被 Session 拒绝；final 目录在 Publish 前不存在。
- 同一目标第二次写入必须返回可由 `errors.Is(err, fs.ErrExist)` 判定的错误，第一次内容不变。绕过规划器直接向 Sink 写 `Foo/SKILL.md`、`foo/SKILL.md`，在大小写不敏感卷上也必须拒绝覆盖；用 t.TempDir 内的 fixture 确认卷行为，不能只按 runtime.GOOS 决定期望。
- 规划器拒绝大小写别名后不进入 Copy；如果实际文件系统还有未预见的路径别名，独占创建失败须触发整次 Abort，不能回退为截断写入，也不能把已部分复制的内容发布。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/projection ./internal/session ./internal/launch -run 'TestCopy|TestSessionDirectories|TestProjectionWrite' -v`

Expected：FAIL。

- [ ] **Step 3: 实现不依赖 agent 的 Sink**

```go
type Sink interface {
	Mkdir(relative string) error
	WriteFile(relative string, data []byte) error
}

type Copier struct{ OpenRoot OpenRoot }

func (c Copier) Copy(ctx context.Context, manifest Manifest, sink Sink) error
```

Copy 重新打开 manifest.Root 并锚定访问，重新枚举/检查清单与每个实际读取文件的路径、大小、hash，使用上限读取并在所有分支 Close。目录或链接被替换导致清单/内容不一致时失败；结果相同的内容替换不声称能被检测。一次只持有一个文件的数据，不把所有投影全文堆进 `LaunchPlan.Files`。最后新增/删除目录也要能检测；检查与复制不是文件系统快照，不能保证捕获最后一次检查之后的源变化。

Session 新增：

```go
func (s *Session) WriteDirectories(paths []string) error
func (s *Session) WriteNewFiles(files []File) error
func (m *Manager) WriteNew(sess *Session, files []File) error
```

复用 `WriteFiles` 的 final→staging 路径验证、`mkdirAllRoot` 与 `chmodRootDir`；但 `WriteNewFiles` 必须以 `os.O_WRONLY|os.O_CREATE|os.O_EXCL` 在锚定的 staging 下创建文件，不能调用现有使用 O_TRUNC 的 `writeRootFile`。保留原 WriteFiles 给生成配置使用，内部仅共享不改变创建语义的校验/写入辅助函数。遇到已存在文件或最终路径 symlink 都返回错误，不先 Remove，也不采用「先 Stat 判断不存在再普通创建」。

不导出 stagingName/sessionsRoot；目录和文件路径都依靠私有 finalRoot，不能信任可被 reporter 修改的 `Session.Root`。

launch 的 Sink 持有 sess 与固定目标前缀：

```go
target := sess.AgentPath("addDir", ".claude", "skills", basename)
```

`Mkdir` 调用 `WriteDirectories`；`WriteFile` 组装一个 `session.File{Path: ..., Data: data, Mode: 0o600}`，交给新增的 `SessionManager.WriteNew`，生产 Manager 委托 Session.WriteNewFiles。同步 fakeSessions 记录独占投影写入；生成 settings 仍走原 SessionManager.Write，测试断言两条路径没有混用。若需要 fake 管理器记录目录操作，给消费方 SessionManager 增加对应方法，生产 Manager 做薄委托。

所有数据都先写 staging；Copy 不负责 Publish/Abort，这两个动作仍由 Service 统一编排。

- [ ] **Step 4: 回归**

Run: `go test ./internal/projection ./internal/session ./internal/launch -v`

Expected：PASS；投影被拒绝时不产生半份目录；精确重名或文件系统别名都不能覆盖已有内容；复制期间发生真实错误时整次不发布。

- [ ] **Step 5: 提交**

```sh
git add internal/projection internal/session internal/launch
git commit -m "feat: copy checked skills into session staging"
```

### Task 13: Claude 完整 settings 与 `--add-dir`

**Files:**

- Modify: `internal/agent/claude/settings.go`、`internal/agent/claude/settings_test.go`。
- Modify: `internal/agent/claude/testdata/settings.golden.json`。
- Create: `internal/agent/claude/testdata/settings-empty.golden.json`。
- Modify: `.gitattributes`。
- Modify: `internal/cli/integration_test.go:assertSettings`。

- [ ] **Step 1: 写 settings golden 与参数测试**

固定 fixture：普通 allowed/native、普通 blocked、settings-only stale；projected 名 `foreign`；installed 允许 `allowed@market`、installed 禁止 `blocked@market`、settings-only `stale@market`、允许但未安装 `missing@market`；Bundled=false。

Expected：

```json
{
  "skillOverrides": {
    "allowed": "on",
    "blocked": "off",
    "foreign": "on",
    "stale": "off"
  },
  "enabledPlugins": {
    "allowed@market": true,
    "blocked@market": false,
    "missing@market": true,
    "stale@market": false
  },
  "disableBundledSkills": true
}
```

另外测试：空全集仍输出 `{}` 而非 null/省略；Bundled=true 必须输出 false；同名多入口全部控制；plugin skill 不写成普通 skillOverrides 开关；projected entries 数量为零时完全不带 `--add-dir`。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/agent/claude -run TestPlan -v`

Expected：旧 JSON 只有 skillOverrides，FAIL。

- [ ] **Step 3: 扩展生成对象**

读取 settings 的输入结构与生成结构分开，避免未知用户字段被写回：

```go
type generatedSettings struct {
	SkillOverrides       map[string]string `json:"skillOverrides"`
	EnabledPlugins       map[string]bool   `json:"enabledPlugins"`
	DisableBundledSkills bool              `json:"disableBundledSkills"`
}
```

- 普通 skill 的全集 = 非 plugin 的 native 扫描名 ∪ settings skillOverrides 键 ∪ projected 名；先 off 后允许名 on。先从 inventory 建立 pluginNames 与 ordinaryNames，允许名若只属于 pluginNames 则不写 on；同名普通入口或 projected 入口仍可正常控制。这样 `Resolved.AllowedNames()` 中的 plugin native 名不会被最后一轮循环重新当作普通 skill 写回。settings 中历史遗留的 plugin 名键可以保留 off，但不能声称它能关闭 plugin。
- plugin 的全集 = inv.PluginIDs ∪ a.options.Plugins；先 false 后允许项 true，不参考 plugin list 的 enabled 当前值。
- `DisableBundledSkills = !a.options.Bundled`。
- JSON 仍 MarshalIndent 加 LF，File.Mode=0600，Plan.Env 为空 map。
- ControlArgs 顺序固定：`--settings <final settings path>`；有 projected 才追加 `--add-dir <final addDir path>`。参数中不能出现 `.staging-*`。

同步 Phase 1 的 `TestPlanIncludesAllowedNameAbsentFromInventory`：它原来明确期望忽略 projected，现在应断言 projected 开启且有 add-dir。把旧无效 `plugin-only` fixture 改为合法两段 ID。不能只更新 golden 后忽略语义断言。

- [ ] **Step 4: 更新并验证 golden**

```sh
go test ./internal/agent/claude -run TestPlan -update
go test ./internal/agent/claude ./internal/cli -v
```

Expected：PASS。检查 diff 后将 `.gitattributes` 扩展到本期 Claude/CLI 的文本 golden，固定 LF；不要统一改写仓库全部文件行尾。

- [ ] **Step 5: 提交**

```sh
git add internal/agent/claude internal/cli/integration_test.go .gitattributes
git commit -m "feat: generate complete Claude isolation settings"
```

### Task 14: launch 接入完整投影流程和结果元数据

**Files:**

- Modify: `internal/launch/launch.go`、`internal/launch/launch_test.go`、`internal/launch/projection.go`。
- Modify: `internal/launch/render.go`。
- Modify: `internal/cli/root.go`。

- [ ] **Step 1: 更新编排记录型测试**

成功路径的固定顺序：

```text
load config → reap → executable → selection → load/merge sets → conflict
→ factory → native/plugin inventory → foreign scan → resolve/inspect
→ Stage → Copy → adapter.Plan → Write(config) → Publish → Report → Handoff
```

foreign scan 使用 `projection.MaxBytes` 作为入口读取上限，拒绝原因随合并结果传入 resolve callback。补组合测试：只存在特殊/超限入口的选中 ID 为 unavailable；同 ID 有普通 Claude native 时仍为 native；有合法的其他 foreign location 时按优先级尝试。fake OpenRegular/Inspector/WriteNew 的调用记录必须证明特殊入口没有读取、超限入口读取未超过上界、两者都未被复制；真实 I/O 错误没有被降级为 warning。

dry-run 从 resolve/inspect 后走 `Preview → Plan → Report`；无 Stage/Copy/Write/Publish/Handoff。none 连 factory/inventory/inspect/Preview 都不调用。

失败测试覆盖：factory、foreign、inspect、Stage、Copy、Plan、Write、Publish、Report、Handoff 与每个外部步骤间的 ctx 取消。Stage 之后失败恰好 Abort 一次；Preview 永不 Abort。旧 Phase 1 清理错误链和 reporter 不可变性测试继续执行。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/launch -run 'TestServiceRun|TestPrepareProjection' -v`

Expected：旧 Run 没有投影步骤，FAIL。

- [ ] **Step 3: 接入服务并保留输出白名单**

将 Service 增加 Inspector/Copier 的消费方接口，CLI 注入 OS 文件系统实现。解析、manifest 与复制句柄只在本次调用存活；句柄关闭失败作为错误或 cleanup warning 保留，不泄漏到后续启动。

Result 增加展示元数据：

```go
type ProjectionFile struct {
	ID   string
	Path string // absolute final path; no body or open handle
}

type PluginSummary struct {
	Allowed  int
	Disabled int
}
```

`Result.ProjectionFiles []ProjectionFile`、`Result.Plugins PluginSummary`、`Result.Bundled bool`。元数据由执行者实际要写的集合计算：Plugins 是 inv.PluginIDs ∪ selection.Plugins[agent] 每项按允许集合 true/false 的数量；与 Task 13 golden 做交叉断言。missing plugin 的文本来自 Inventory 已使用 installed 集合算出的告警，不由 PluginIDs 差集猜测。

`launch.Summary` 补 Projected、Unavailable（含 ID/Reason）、Missing，并作为唯一摘要计数实现由 CLI 使用；不要维护一份只供测试、另一份实际渲染的重复统计。

`cloneResult` 深复制 ProjectionFiles、全部原因/告警数据与现有 plan/inventory/resolved。Report 不得获得 Manifest.Source、投影文件内容或能操作 staging 的 Session；仍可获得 skope 生成的配置正文。实际 argv/env 仍在 Report 前独立保存，避免 reporter 改变 handoff。

- [ ] **Step 4: 回归**

Run: `go test ./internal/launch ./internal/agent/claude ./internal/session ./internal/projection ./internal/cli -v`

Expected：PASS；active dry-run 恰好一次 plugin list、一次来源规划，所有 projected final path 与复制目标一致。

- [ ] **Step 5: 提交**

```sh
git add internal/launch internal/cli/root.go
git commit -m "feat: orchestrate Claude projection and complete launch results"
```

### Task 15: 完整摘要、dry-run 与全部输出边界

**Files:**

- Modify: `internal/cli/launch_cmd.go`、`internal/cli/root.go`、`internal/cli/list_cmd.go`、`internal/cli/version.go`。
- Create: `internal/cli/render.go`、`internal/cli/render_test.go`。
- Modify: `internal/cli/root_test.go`、`internal/cli/list_cmd_test.go`、`internal/cli/version_test.go`。
- Create: `internal/cli/testdata/claude-summary.golden.txt`、`internal/cli/testdata/claude-dry-run.golden.txt`。

- [ ] **Step 1: 写渲染与输出泄漏测试**

摘要 fixture 覆盖四状态、plugin missing、bundled、三类 collision、reap/projection warning。输出形状：

```text
→ Skillset [dev] for claude
  skills: 1 native, 1 projected, 1 unavailable, 1 missing
           unavailable: unsafe (引用目录外内容)
           missing: absent
  plugins: 1 allowed, 2 disabled (Claude may enable dependencies of allowed plugins)
  bundled: off
→ Launching claude
```

实际 fixture 的数量与 generated settings 精确对应，不能以此示意文本替代语义断言。测试至少包括：

- 外部值中的 ESC/OSC、换行、tab、C1、bidi 在列表、错误、warnings、collision、version 与路径中都被转义。
- 内部排版换行与 tabwriter 对齐保留；不能对整个 writer 套 Escape 把格式换行全变成 `\\x0a`。
- dry-run 输出最终 argv、Plan.Env 的键与脱敏值、owner.json/生成配置/投影文件相对路径、生成 JSON、摘要与告警。
- 继承 env 中有 `AUTH_TOKEN=SECRET_SENTINEL`，投影正文有 `BODY_SENTINEL`；输出都不出现。即使 Plan.Files 以后扩展，也只有明确的生成配置类型可以输出内容。
- `Plan.Env["API_KEY"]` 输出 `<redacted>`，普通新增值转义；键名稳定排序。
- none dry-run 的环境变化清单为空，不打印 Result.Env；没有 session files 或 settings 内容。
- JSON 内恶意 skill 名安全显示且原 Plan.Files 字节不变；stdout 失败仍返回 1。
- Cobra 未知命令/参数错误不绕过安全出口，不重复打印 Error。

- [ ] **Step 2: 运行红灯**

Run: `go test ./internal/cli -run 'TestRender|TestClaudeCommand|TestList|TestExecute|TestVersion' -v`

Expected：原始文本、旧摘要与 dry-run 输出使新增断言 FAIL。

- [ ] **Step 3: 统一渲染入口**

将 reportLaunch/reportResolution/reportWarnings/reportCollisions/reportDryRun/sessionRelativePath 从 launch_cmd.go 移到 render.go，命令文件只负责参数和 runner。外部字符串插值前用 termsafe.Escape，内部固定文本正常输出。

root 使用 `SilenceErrors:true`；`Application.Execute` 捕获错误后统一 `fmt.Fprintf(stderr, "Error: %s\n", termsafe.Escape(err.Error()))` 并返回 1。下层保留原始、受限的错误字段，不把已经 Escape 的字符串再传入 Escape。proc stderr 先截断原始 2 KiB，最终在这个出口转义；独立 StderrExcerpt 只供直接展示它的路径使用，二者不叠加。

dry-run：

1. argv 每项保留空值/空格边界，可以用 JSON 数组或逐项引用显示；保证 §9.1 所列字符全部转义，`strconv.Quote` 单独使用不作为充分证据。
2. 环境只遍历 Plan.Env，永远不遍历 Result.Env；生成 OpenCode 内容的未来入口必须提供注入字段，不能直接打印继承的完整 JSON。
3. 文件列表包含 `owner.json`、Plan.Files 与 ProjectionFiles 的排序去重并集；不输出 owner 正文。所有路径先做 sessionRelativePath 校验。
4. 只打印当前明确的 `claude/settings.json` 正文；先逐行处理其中原始 Unicode 控制字符，保留格式化 JSON 自己的换行。不读磁盘文件，不输出投影 .md/脚本/二进制内容。
5. dry-run 结尾不打印 Launching，不在此处执行任何探测或文件写入。

list 在写进 tabwriter 前转义 Name/Description/缺失文件路径。version 中 ldflags 传入的字符串也视作外部数据。错误字段可能包含 `%q` 已引用内容，只保证最终不出现原始控制字符，不刻意还原后再转义。

- [ ] **Step 4: 更新 golden 并回归**

```sh
go test ./internal/cli -run TestRender -update
go test ./internal/cli ./internal/termsafe -v
```

Expected：PASS；只用当前包一个 `-update` flag（若已有则复用），golden 稳定化 session 根，不把随机目录固化进文件。检查 byte-level 测试确认危险控制字符不出现在外部字段输出中。

- [ ] **Step 5: 提交**

```sh
git add internal/cli .gitattributes
git commit -m "feat: render complete and terminal-safe Claude launch previews"
```

### Task 16: fake agent 端到端与回归矩阵

**Files:**

- Modify: `internal/cli/integration_test.go`。
- Create: `internal/cli/phase2_integration_test.go`。
- Modify if needed: `internal/testutil/probe_test.go`。

- [ ] **Step 1: 写完整 Unix 启动 fixture**

沿用 `newIntegrationFixture`、`withEnv`、BuildSkope、BuildFakeAgent。将 HOME/USERPROFILE/CLAUDE_CONFIG_DIR/CODEX_HOME/SKOPE_HOME 指向临时目录；探测日志与最终启动日志分开。生产命令不能探测到真正用户 plugin 或 skill。

增加 native allowed/blocked、`.agents/skills/foreign`（含资源和空目录）、外链 rejected、unknown missing。plugin fixture 由 fake list 返回 Task 2 的已验证形状和当前临时安装路径；允许/禁止各一个，并加 settings-only stale。配置 args 和 user args 中分别放不同 marker。

- [ ] **Step 2: 写失败测试并执行**

Run: `go test ./internal/cli -run 'TestIntegrationPhaseTwo' -v`

Expected：新增集成断言先暴露装配缺口；如实现已满足，应记录直接绿灯，不伪称必有失败。

| 场景 | 必须断言 |
|---|---|
| 正常启动 | probe 恰好一次；argv=config args+user args+settings+add-dir；退出码 23 透传 |
| 探测环境 | cwd 与启动一致；只含继承 env，没有会话注入；probe argv 只有三个固定 token |
| session | owner、settings、投影资源和空目录完整；没有 symlink；Unix 权限正确 |
| 白名单 | allowed/foreign on，blocked/stale off；plugin true/false 与 bundled 正确 |
| 无有效投影 | 没有 add-dir 参数或空投影目录 |
| active dry-run | probe 一次；检查完整但不写新会话、不改最终 fake output、不输出 BODY_SENTINEL |
| 回收 | 退出后下一次 dry-run 删除已结束 owner 的旧会话；不把 dry-run 误断言为零删除 |
| none | 损坏 skillsets 和损坏 plugin JSON 均不影响旁路；无 probe/scan/projection；配置损坏仍失败 |
| 五种冲突 | config/user 两种来源；probe 和最终 agent 都未启动；值不泄漏 |
| plugin 枚举失败 | 非零/非法 JSON/非法字段/超时/超限；无 session/handoff |
| 未信任 workspace 的 suppressed 占位 | 使用 Task 2 占位 fixture；active dry-run/launch 均退出 1，不调用 Plan/Stage/Handoff，不输出 notes sentinel；错误提示先在 Claude 独立完成信任。none 仍无 probe 并旁路成功 |
| missing plugin/skill | warning，启动继续；不能隐式启用额外 plugin |
| projection Rejection | unavailable 后继续；被拒文件不出现在会话 |
| 外来 SKILL.md 为 FIFO/超大文件 | 完整 foreign scan → resolve → dry-run 路径有限时返回 unavailable；FIFO helper 不阻塞，普通文件读取保持上界 |
| 两个来源分别提供 `Foo`/`foo` | active 与 dry-run 都只有一个 projected；后者 target-conflict，首项内容保持原样 |
| 投影目标已存在或存在路径别名 | 独占写入失败，原文件不被截断，staging 清理且不 handoff |
| projection I/O/写入失败 | 退出 1、staging 清理；不发布、不 handoff |

helper timeout/输出超限用 proc 单测完整证明；CLI 只需代表性错误传播，不为每条端到端测试等待完整 15 s。Unix 原生 handoff 集成继续在 Windows 跳过；新增 Windows dry-run/probe 集成使用真实 helper `.exe`，不得整体跳过 Phase 2 测试。

- [ ] **Step 3: 修复实际缺口**

只修上述行为与既有行为的回归；更新旧 `assertSettings` 对 plugin/bundled 缺失的断言。`TestClaudeCommandDryRunReportsArgvAndSessionFilesWithoutEnvironment` 改成「只显示环境变化」的测试名与断言。

使用 `os.SameFile`/canonical path 比较 fixture 身份，保留 Phase 1 对 macOS 短路径和 Windows 大小写的修复。不得删除已有 Phase 1 错误测试来让全量测试通过。

- [ ] **Step 4: 本地全量回归**

```sh
go test ./...
go test -short ./...
go vet ./...
go build ./...
```

Expected：全部 PASS；Linux/macOS CI 另跑 race。长测试只执行一次完整验证，后续仅在改代码或新增失败时重跑相关检查。

- [ ] **Step 5: 提交**

```sh
git add internal/cli internal/testutil
git commit -m "test: cover complete Claude isolation end to end"
```

### Task 17: 质量门禁、真实 Claude 验收与文档交付

**Files:**

- Modify: `README.md`。
- Modify: `docs/verification.md`（新增 Phase 2 记录，保留第 3/10 条证据）。
- Modify: `docs/superpowers/plans/2026-09-05-phase2-claude-completion.md`（勾选、记录偏差）。
- Modify only when needed: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`。

- [ ] **Step 1: 冻结类型与依赖审计**

对照基线 `27d22be` 检查六个冻结类型没有字段/方法变更，`cmd/skope/main.go` 无业务改动。检查 leaf 不 import agent，adapter 不 import cli/launch/其他 adapter，skill 核心不 import projection/os，根命令 help 不读取配置或执行 probe。

若实现确需改变冻结结构，先回到 spec 修订流程；Phase 2 不修改 Adapter 接口。记录 Options、registry factory、外来扫描行前移、proc 平台文件等已批准设计补充，避免下一期重新推断。

- [ ] **Step 2: 格式化与本地质量门禁**

```sh
gofmt -w internal/termsafe internal/proc internal/projection internal/agent/claude internal/skill internal/host internal/session internal/launch internal/cli internal/testutil
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 fmt
make check
go test -count=1 -coverprofile=coverage.txt ./...
go tool cover -func=coverage.txt
git diff --check
git status --short
```

Windows 中 make 按 README 从 Git Bash 运行；PowerShell 覆盖率参数若有解析问题，整项用单引号。Expected：格式/vet/lint/test/build 全部退出 0；报告覆盖率与新增低覆盖业务分支。项目目标至少 80%，不能用排除业务文件或无意义 getter 测试抬数字。检查格式化 diff，仅保留本期必要改动。

- [ ] **Step 3: 创建/更新 PR 并确认 CI**

按会话授权推送功能分支并建立以 main 为 base 的 PR，描述行为、spec 范围、验证命令和 Windows 边界。当前 CI 的 push 只监听 main，功能分支通过 pull_request 触发。确认 Ubuntu/macOS race、Windows test/build、lint 全绿并记录 run URL；只 push 分支不能称 CI 已验证。

- [ ] **Step 4: 用 skope 验收真实 Claude**

先在执行真实验收的 Linux/macOS/WSL 环境中，从实施 worktree 的仓库根构建本次代码。确认 cmd/internal/go.mod/go.sum 的实现改动均已提交；文档可以待本 Task 收尾提交。`make check` 仅在仓库根生成 skope，不会安装到 PATH，不能用裸 `skope` 或切到 fixture 后的 `./skope` 代替本次产物。

```sh
SKOPE_BUILD_COMMIT="$(git rev-parse HEAD)"
SKOPE_BUILD_DIR="$(mktemp -d)"
SKOPE_EXE="$SKOPE_BUILD_DIR/skope"
GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go build -ldflags "-X main.version=phase2-$SKOPE_BUILD_COMMIT" -o "$SKOPE_EXE" ./cmd/skope
"$SKOPE_EXE" version
go version -m "$SKOPE_EXE"
```

`SKOPE_BUILD_DIR` 必须为本次创建的绝对目录，故 `SKOPE_EXE` 在后续切换 cwd 后仍指向相同文件。Expected：version 输出 `skope phase2-<SKOPE_BUILD_COMMIT>`，build info 的目标 OS/架构与执行验收的环境一致；任一构建或核对失败都不继续实验，不回退 PATH 中的旧版本。

复用 Task 1–2 的隔离 fixture，加手写 skillsets：native + foreign + blocked plugin + bundled=false。确认 FIXTURE 是绝对路径，实验环境仍重定向到 fixture 后执行：

```sh
cd "$FIXTURE/repo"
"$SKOPE_EXE" claude -s phase2 --dry-run
"$SKOPE_EXE" claude -s phase2 -- -p /native-check --max-turns 2 --output-format text
"$SKOPE_EXE" claude -s phase2 -- -p /projected-check --max-turns 2 --output-format text
"$SKOPE_EXE" claude -s phase2 -- -p /blocked-check --max-turns 2 --output-format text
"$SKOPE_EXE" claude -s phase2 --dry-run
```

另用 Task 2 验证过的实际 plugin namespace 调用允许/禁止 plugin skill，所有调用继续使用 `"$SKOPE_EXE"`。验证 bundled 开关与复制资源读取、退出后会话保留及下一次启动回收。记录 `SKOPE_BUILD_COMMIT`、产物绝对路径、version/build info、Claude 版本、平台、完整 fixture、完整命令和确定性观察；不把 dry-run JSON 正确等同于 agent 实际遵守配置。可以额外记录文件 SHA-256，不能只记录可能指向其他产物的 PATH 命令名。

若只能使用 WSL→Windows bridge，`SKOPE_EXE` 必须是在 WSL 中从本次提交构建的 Linux 产物；bridge 只作为配置中的 Claude command。桥接需要为探测返回可读、可转换的安装路径，并转换 settings/add-dir；任何路径重写都记录为实验桥接限制，不写进产品实现。原生 Unix probe/handoff 仍以 CI 为证据；真实 Claude 能力结论明确标注宿主平台。

- [ ] **Step 5: 更新用户文档与实施记录**

README 状态改为 Phase 2 complete，并给一个 `plugins.claude` + bundled + foreign skill 示例。明确：

- plugin 允许是整体 plugin；不会由 skills 中出现某个 ID 自动开启。
- 本期外来来源只有两条已前移的全局行；其他来源仍属后续阶段。
- 投影目标按保守规则拒绝大小写别名；`Foo`/`foo` 在所有平台都不能同时投影，文件写入不覆盖已有内容。
- dry-run 会执行一次 plugin list 和来源检查、可能回收旧会话，但不创建新会话。
- Windows 最终 handoff、`.cmd` 垫片仍未交付；TTY 选择器和管理向导仍待 Phase 5。
- 投影权限固定为 0600/0700；Windows 使用用户目录默认 DACL，不能把 POSIX mode 当作 Windows ACL 验证。

docs/verification.md 追加真实与自动证据，计划记录实施偏差；没有证据的步骤保持未勾选。

- [ ] **Step 6: 提交交付文档**

```sh
git add README.md docs/verification.md docs/superpowers/plans/2026-09-05-phase2-claude-completion.md
git commit -m "docs: close Phase 2 after Claude integration verification"
```

- [ ] **Step 7: 干净 clone 验证**

从已提交版本建立一次性 clone，运行 make check 与包含 fake plugin probe、foreign projection 的 dry-run fixture；在 clone 中重新绑定其构建产物的绝对路径，不沿用原 worktree 的 SKOPE_EXE。确认不依赖工作区未跟踪文件；不提交 coverage.txt、二进制、dist、临时输出或 AGENTS.md。Windows 清理前核验绝对目标确实是本次临时 clone，使用同一 PowerShell 的 `Remove-Item -LiteralPath`；不要跨 shell 拼装删除命令。

## 验收对应表

| spec / 风险 | Task | 自动或真实证据 |
|---|---:|---|
| §7.5 第 3、10 条 | 1–2 | docs/verification.md 的版本、fixture、命令、观察 |
| §9.2 15 s、每路 4 MiB、EOF、继承环境、终止树 | 4–6 | proc/probe helper 与三平台测试 |
| §7.1 installed ∪ settings ∪ allowed 的 plugin 开关 | 7、13 | inventory/parser tests + settings golden |
| §5.3/§5.5 plugins/bundled 与多 set 并集生效 | 8、13、16 | Options/factory tests、真实 JSON/argv |
| §6.2 Claude 五种冲突与值不泄漏 | 8、16 | config/user 两来源，none 旁路 |
| 外来来源能实际进入解析 | 9、16 | 两条全局 root fixture |
| 外来入口先检查类型/有界读取，拒绝原因不丢失 | 9、11、14、16 | special/limit fake reader、Unix FIFO helper、完整扫描链路 |
| §4.4 四状态、优先级、plugin/command 限制 | 10–11 | 解析与 Inspector 矩阵 |
| §8.3 根内链接、外链拒绝、2000/20 MiB、文本告警 | 10–12 | inspect/copy tests，真实资源读取 |
| 大小写目标冲突、独占创建防覆盖 | 10–12、16 | 跨来源 Foo/foo、skill 内文件/目录别名、fs.ErrExist 与原内容断言 |
| 冻结接口与安全 staging | 8、12、14、17 | 类型 diff、生命周期记录、越界/清理测试 |
| §6.6 四类计数、插件实际写入计数 | 13–15 | settings 与摘要交叉断言 |
| §6.3 dry-run 无创建、无正文泄漏、只显示 env 变化 | 14–16 | 文件快照、sentinel、probe 日志 |
| §9.1 全部当前输出边界 | 3、15–16 | 控制字符/脱敏/统一错误出口测试 |
| §10 本期 Claude 错误行 | 7–16 | parser/proc/launch/CLI/integration |
| §14.7 本地与三平台门禁、真实验收产物身份 | 17 | make/coverage/CI run/干净 clone、SKOPE_EXE 与提交号/build info |

## 不在本期实现

- Codex/OpenCode adapter、其冲突参数与原生配置控制；其真实验证门禁保留在 Phase 3/4。
- 尚未前移的 Codex 项目/admin/plugin 扫描、OpenCode 配置 paths/urls 与插件管理。
- TTY 选择器、create/edit/delete/skills/doctor、TOML 写入与交互 UI。
- Windows 最终 agent handoff、Ctrl 事件、`.cmd` 垫片、退出后清理与发布渠道。
- 自动修改真实用户配置、下载/更新/卸载用户 plugin、解析 plugin 依赖闭包或 managed policy。

## 文档核对来源与待验证项

- 2026-09-05 使用 Context7 先执行 `library 'Claude Code' <详细查询>`，再执行 `docs /websites/code_claude <详细查询>`。官方文档支持 add-dir skills、plugin namespacing、enabledPlugins 和 plugin skill 不受 skillOverrides 控制，但未提供足以替代本仓库第 3/10 条实验的完整、版本固定 JSON schema。[Skills](https://code.claude.com/docs/en/slash-commands)、[Plugins reference](https://code.claude.com/docs/en/plugins-reference)、[Settings reference](https://code.claude.com/docs/en/settings-reference)。
- 官方 plugin 文档出现 skills-directory plugins，说明缓存路径不能凭经验写死；Task 2 必须核对正在支持的 Claude 版本是否具有该行为。[Plugins reference](https://code.claude.com/docs/en/plugins-reference)。
- Windows 辅助进程选择创建时绑定 Job，避免创建后再 Assign 的间隙；此处是实现选择，不改变 spec §8.4 的后续 Windows handoff 设计。[Microsoft](https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812)。
