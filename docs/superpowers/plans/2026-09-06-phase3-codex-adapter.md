# skill-scope Phase 3（Codex adapter）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 Phase 2 基线上交付 `skope codex -s <set>`，按 canonical `SKILL.md` 路径全集生成会话级 true/false 规则，控制 Codex plugin/bundled，正确报告不可用的外来 skill，并通过 fake agent 和真实 Codex 验收。

**Architecture:** 保留六个冻结类型及现有 launch 生命周期。`skill` 补齐目录发现和按显式根进行的有界外来扫描；`agent/codex` 只读安装元数据、生成原生 inventory 和 TOML 控制参数；CLI 显式装配两种 adapter 及目标相关的外来来源。普通 skill 的开关依据选中 ID 对应的全部 Codex 路径，plugin 单独按整个插件控制。

**Tech Stack:** 沿用 Go 1.24.0、Cobra v1.10.2、go-toml/v2 v2.4.3、yaml.v3 v3.0.1、x/sys v0.41.0、标准库 `testing` / `testing/fstest`。不新增生产依赖。

---

**Spec 对应：** `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §3、§4.1–§4.4、§5.3/§5.5、§6.1–§6.3/§6.6、§7.2/§7.4/§7.5 第 1、6、9 条、§8.1/§8.2、§9–§12、§14.3/§14.7。

**执行状态：** Task 1–2 本地 Linux 门禁及独立审查已通过（基线 eb3cdad）；Task 3 已统一契约，Task 4–13 待实现。真实证据为 Codex CLI 0.153.1 的 53+3+3 组发现/plugin 对照及 Task 1 路径对照；Windows/Junction、真实交互模型、认证远端闭环仍未验，不混作本地实现门禁。

**执行技能：** 使用用户指定的 `@superpowers:subagent-driven-development` 逐任务实施并作独立 spec→quality 审查，编码参考 `@karpathy-guidelines`；行为变更使用 `@superpowers:test-driven-development`，失败使用 `@superpowers:systematic-debugging`，最终交付使用 `@superpowers:verification-before-completion`。每个测试矩阵逐行完成红灯、最小实现、绿灯，再处理下一行；每一步控制在约 2–5 分钟，真实实验和全量门禁按实际耗时记录。

新增测试使用标准库 `testing` 和 `xxx_test` 黑盒包，沿用现有 fixture helper；依赖私有生命周期的已有包内测试保持原位置。执行命令中的 `-run` 对应新增测试名前缀，必须确认实际匹配到测试，不能把 `[no tests to run]` 当作通过。

## 基线与实施约束

- 基线为 `main` 的 `b3298576442555fe40305cb9f5b1a4e8bbc2a448`，Phase 2 PR #2 已合并。规划时 `go test -short ./...` 全部通过；这只是已有实现的基线证据。
- 计划位置服从仓库约定 `docs/superpowers/plans/`。当前工作区的未跟踪 `AGENTS.md` 属于用户，不修改、不提交；不要使用 `git add .`。
- 本计划进入版本控制后，实施前使用 `@superpowers:using-git-worktrees` 从包含 Phase 2 的最新 main 创建 `codex/phase3-codex-adapter`。计划编写不建立实现 worktree，也不执行产品改动。
- 仓库没有 `.codegraph/`，直接使用 `rg` 和精确文件读取，不自动建立索引。
- 六个冻结类型为 `Skill`、`Location`、`Adapter`、`Capabilities`、`LaunchPlan`、`Inventory`。本方案不修改它们；若真实实验要求修改，必须先修订 spec §4.2/§7，再调整计划并回归 Claude。
- Linux/macOS 继续使用现有 exec handoff。Windows 产品支持扫描、规划和 active dry-run，正常运行读取真实用户来源；自动测试的真实 binary 仅运行 none/help，active（含 dry-run）由注入临时 CodexPaths/handoff 的应用测试覆盖，不能靠 HOME/USERPROFILE 隔离 Known Folder。最终 handoff、生产进程身份检查、`.cmd` 解析仍为 Phase 6。
- 真实实验使用临时 HOME、CODEX_HOME、SKOPE_HOME、CLAUDE_CONFIG_DIR 和独立仓库。生产 skope 不重定向 CODEX_HOME，不复制用户认证文件，不修改 Codex 配置或插件安装状态。

| 当前文件 / 函数 | 已有行为 | 本期接法 |
|---|---|---|
| `internal/skill/scope.go:foreignGlobalRoots` | 两条 Codex 全局来源已启用 | 复用根定义，增加项目/admin 根，不复制出另一套发现规则 |
| `internal/skill/scan.go:ScanRoots` | 支持 Root 的可见 agent、PluginID、NamePrefix；skill 根只读直接子项 | 原生目录扫描复用；CodexRecursive 已实测固定，Claude 零值保留直子项 |
| `internal/skill/foreign.go:ScanForeignGlobals` | 有界读取，但根固定且构造 Location 时未保留 Scope/PluginID | 抽取 `ScanForeignRoots`，保留完整来源元数据和拒绝原因 |
| `internal/skill/resolve.go:resolveCandidate` | `Projection=false` 时统一返回 projection-unsupported | 区分 plugin-disabled、plugin-only、command-only 与普通外来来源 |
| `internal/agent/agent.go` | Inventory 没有路径 denylist 或用户 TOML 字段 | 路径从 Skills/Locations 计算；不把路径塞进 SkillNames |
| `internal/cli/root.go:dependencies.runLaunch` | 请求级 factory 只注册 Claude | 注册 Codex，Options 复制选中 plugins/bundled；构造不得读配置或探测 |
| `internal/launch/launch.go:Service.Run` | 原生 inventory 后无条件扫两条外来全局来源 | 注入目标相关的外来扫描；Codex 启动能发现 Claude 普通 skill/command |
| `internal/cli/conflict.go` | 只检查 Claude；错误固定写 Claude | 增加 Codex 参数提取和来源跟踪，错误不含值 |
| `internal/cli/render.go:reportResolution` | 插件提示和 plugin-disabled 文案写死 Claude | 根据目标输出；Codex dry-run 只展示生成的 argv 与 owner.json |
| `internal/testutil/build.go` | 已有 BuildFakeAgent、BuildSkope | 直接复用，Codex inventory 不新增辅助 CLI probe |

## 设计决策

### 路径白名单与原配置

1. **普通 skill 按路径控制。** 从 `Resolved.Entries` 中 `StateNative` 的 ID 回查 `Inventory.Skills`，允许该 ID 的全部普通 Codex location。不能使用 `AllowedNames()` 决定哪些路径保留，否则两个 ID 的 frontmatter 同名会被一起允许。
2. **发现身份与控制身份分开。** Inventory 继续按 `(Source, DiscoveryPath)` 去重，保留 symlink/Junction 别名；控制参数按重新 canonicalize 的绝对 `SKILL.md` 文件路径去重。不写目录路径，不用 `strings.EqualFold` 合并原生路径，不把投影大小写规则用于 Codex。
3. **同一 canonical 路径存在允许和禁止别名时，允许优先并告警。** 这是路径开关无法区分同一目标的限制。选中任一别名后，其余别名共享这个目标；既不生成互相矛盾的条目，也不声称仍可按入口隔离。Linux canonical 对照已通过，Windows/Junction 实机结论仍待验。
4. **每次显式生成普通和安装 plugin 的路径全集 true/false。** User deny 跨层累积，CLI [] 不清除；真实 path=true 覆盖 User path/name 及组合。空 inventory 才生成 []；全选必须仍写所有 true。
5. **允许 plugin 显式写 true，其余写 false。** 全集为实际安装 ID、有效配置中的 plugin 键、允许列表的并集。允许未安装项仍写 true 并告警 missing；installed 要求配置键及有效 active cache；配置键或 marketplace 候选单独不等于安装。plugin 按整体启用，其全部 skills 也进入路径全集，按 plugin 允许状态写 true/false，不依赖 skill ID 选择。
6. **bundled 独立控制。** 始终生成 `skills.bundled.enabled=<bool>`；`.system` 等真实版本的 bundled 根不进入普通 skill denylist，也不成为 Claude 投影候选。不得因磁盘有 bundled 缓存就在 `bundled=true` 时又逐路径禁用它。

上述规则已同步 spec §3/§4.3/§7.2/§7.4。选择 true/false 全集是实测修正；本地限定及 remote_plugin=false 是固定源码支持的项目选择；认证远端门禁归 Phase 5。bundled=true 只允许 system 来源，保留 User 单项 deny。

### 发现范围与控制面边界

- **根范围跟随已验证的 Codex。** 基础集合是全局 `.agents/skills`、CODEX_HOME/skills，项目 cwd 到 git 根每一级的 `.agents/skills`、`.codex/skills`，Unix `/etc/codex/skills` 和实际安装 plugin 根。无 git 根只扫描 cwd 项目层；`.git` 文件形式的 worktree 也必须识别。
- **原生发现必须完整。** Task 2 已固定根内部递归、跳隐藏、跟随中间目录/skill 链接、已有 SKILL 继续递归；Task 4 显式 CodexRecursive 策略，防环且保留 discovery 身份。不能靠告警继续启动不完整清单。
- **配置只读投影。** Codex TOML 允许其他业务键，不能用 skope 配置的 `DisallowUnknownFields()` 拒绝整个 Codex 文件；只验证本期读取的插件/skill 规则和安装元数据形状。语法错误或管理字段错误 fail-closed，不回显配置正文、凭据或任意解码值。
- **配置层必须覆盖能引入插件的来源。** 读取 User、system/admin、祖先项目配置层，不读取运行时 profile。设计按所有适用层的 plugin ID 并集关闭，不能只读用户 config 而漏掉项目新增 ID；无需重建模型、权限、MCP 等无关键的最终配置。
- **活动隔离拒绝变更 cwd 和 profile 的参数。** 本期拒绝 `-C`/`--cd` 与 `-p`/`--profile` 及分离、等号、短旗附着形式；用户先切换目录再启动。拒绝通过 `-c` 设置受保护的 skills/plugins 顶层或子树，以及影响这些来源的 profile/发现根设置。该限制在 Task 3 写入 spec §6.2；`none` 完整透传。选择拒绝是为了避免实现另一套 Codex cwd/profile 解释器。
- **profile 明确拒绝。** CLI 0.153.1 运行时 profile 采用独立文件；本期活动拒绝 -p/--profile。旧 config.profile 不支持，读取到即 unsupported-source，不实现 profile 解释器。
- **透传中的独立 `--` 不能吞掉控制参数。** 活动隔离拒绝仍留在 agent argv 内的独立 `--`，因为末尾追加的 `-c` 会成为位置参数。skope 自己消费的首个 `--` 不冲突。用户可用 skope 分隔符开始普通透传。
- **外来来源按目标切换。** Claude 目标补齐已验证的 Codex 普通目录和 Codex 安装 plugin 的身份信息；plugin 仍不可投影。Codex 目标扫描已交付的 Claude 普通目录和 legacy commands，用于 unavailable 分类，不调用 Claude plugin list。
- **阶段性来源限制明确保留。** 为 Codex 枚举其他 agent 的完整 plugin inventory 需要额外 agent 探测，不属于本期启动依赖；非目标 Claude plugin 枚举统一留到 Phase 5 的完整候选全集。Task 3 在 §14.5 记录该项，README 明确 missing 指本期已接入来源中无记录。OpenCode 独有来源仍为 Phase 4，不从总 spec 删除。

### 接口与生命周期

```go
// Task 4: internal/host/skill_roots.go（非冻结值类型）
type CodexPaths struct {
    Home, CodexHome string
    AdminSkillRoots, SystemConfigPaths []string
}
func ResolveCodexPaths(env Env) (CodexPaths, error)
// 平台 home helper 位于 skill_roots_unix.go / skill_roots_windows.go。
// Unix env.Home；Windows KnownFolderPath(&windows.FOLDERID_Profile, 0)。

// Task 4: internal/skill/codex_scope.go
func (s Scanner) CodexRoots(host.Env, host.CodexPaths) ([]Root, string, error)
func (s Scanner) ScanCodex(host.Env, host.CodexPaths) (ScanResult, error)

// Task 5: internal/agent/codex/catalog.go；这些消费方接口本任务独立定义。
type CatalogSnapshot struct {
    PluginIDs, InstalledIDs []string
    SkillRoots []skill.Root
    Warnings []string
}
type SourceReader interface {
    Read(context.Context, host.Env, host.CodexPaths) (CatalogSnapshot, error)
}

// Task 5: Catalog 的最小消费接口；NewCatalog 不读文件。
type ManifestReader interface {
    ReadCodexManifest(string) (skill.CodexManifest, error)
}
func NewCatalog(skill.FileSystem, skill.RegularFileOpener, ManifestReader) Catalog
// Catalog 满足 SourceReader；CLI 传 OSFileSystem、安全 opener、scanner。

// Task 6: internal/agent/codex/adapter.go
type SkillScanner interface {
    ScanCodex(host.Env, host.CodexPaths) (skill.ScanResult, error)
    ScanRoots([]skill.Root) (skill.ScanResult, error)
}
type Canonicalizer interface { EvalSymlinks(string) (string, error) }
type Options struct {
    Plugins []string
    Bundled bool
    ResolvePaths func(host.Env) (host.CodexPaths, error)
}
func New(scanner SkillScanner, sources SourceReader, paths Canonicalizer, opts Options) Adapter
```

CLI 显式注入 host.ResolveCodexPaths；测试注入返回临时 CodexPaths 的纯函数。resolver 在 Inventory 内只调用一次，结果传给普通 scanner 与 Catalog；foreign composer 同理。构造 New/registry/help/none 不调用 resolver，不读取 agent 元数据，不改变冻结 host.Env 或 Claude home。CodexPaths 切片在保存/返回时复制；无可变全局。

`New` 深复制切片，Inventory 不缓存结果；Plan 只使用此次 inv、resolved、不可变 Options 和 canonicalizer，不重新读取配置或枚举缓存。Canonicalizer 只复核文件路径，失败即停止；不以字符串清洗替代真实链接解析。Codex Plan 不使用 session 路径，`Files` 和 `Env` 为空；真实 launch 仍通过已有 Stage/Publish 记录 owner.json，避免给本期增加另一套无会话生命周期。

```go
// internal/launch/launch.go：替换未冻结的消费方接口。
type ForeignScanner interface {
	ScanForeign(context.Context, host.Env, skill.Agent, int64) (skill.ScanResult, error)
}

// internal/skill/foreign.go：复用已有有界读取。
func (s Scanner) ScanForeignRoots(roots []Root, maxBytes int64) (ScanResult, error)
```

`internal/cli/sources.go` 实现 ForeignScanner，组合 scanner 与 Codex Catalog；不得让 launch import 某个 adapter，或让两个 adapter 互相 import。`ScanResult` 可增加 `Warnings []string`，foreign 合并时复制告警及 Rejections，冻结 Inventory 不加字段。

## 任务顺序

`1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13`

Task 1–3 是事实与契约门禁；全部完成后才进入 adapter 实现。Task 4–10 建立扫描、规划和编排，Task 11 注册用户入口，Task 12–13 完成自动和真实验收。文档中的 Go 片段明确函数/数据形状或完整关键算法；其余处理由紧邻的输入输出矩阵限定，不允许以「补充校验」作为未说明的任务。

### Task 1: 验证 Codex 路径禁用与跨层数组语义

**2026-09-06 实测偏差（覆盖本计划后续旧的纯 denylist/数组替换假设）：** Codex CLI 0.153.1 的 User path/name 禁用不被 CLI 空数组清除；显式 canonical path=true 可恢复允许项。产品必须枚举普通路径全集写 true/false，spec §7.2 已修订。Linux skills/list、debug prompt-input、bundled 缓存、参数解析、运行时 profile 对照已完成；Windows Known Folder 忽略 HOME 重定向，原生 Windows/Junction 与真实交互模型调用仍未验。Task 3 已同步后续 Task 6–13 的旧片段，不能照抄仅为禁止项写 false 的方案。证据见 docs/verification.md 第 1、9 条及 testdata/verification/README.md。

**Files:**

- Modify: `docs/verification.md`（第 1、9 条）。
- Create: `internal/agent/codex/testdata/verification/README.md`（仅版本、无秘密 fixture 构建步骤和观察）。
- Modify when evidence differs: `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §3/§7.2。

- [x] **Step 1: 固定实验二进制与观察方式**

记录真实 Codex 的绝对路径、`--version`、平台及安装方式。先运行该二进制的 `--help`、`exec --help`；只有实际帮助列出的命令和参数才能写入实验脚本。不以桌面 app 版本代替 CLI 版本，不使用 PATH 中身份不明的第二个 Codex。

以下 fixture 命令在 Linux/macOS/WSL 的 POSIX shell 执行，Windows 原生用对应 PowerShell 路径和链接方式。实验变量不复用 HOME 等系统变量作普通临时变量名；HOME 重定向仅限实验子 shell。

```sh
PHASE3_FIXTURE="$(mktemp -d)"
mkdir -p "$PHASE3_FIXTURE/home/.agents/skills/allow" "$PHASE3_FIXTURE/home/.agents/skills/block" "$PHASE3_FIXTURE/codex" "$PHASE3_FIXTURE/repo"
git -C "$PHASE3_FIXTURE/repo" init
cat > "$PHASE3_FIXTURE/home/.agents/skills/allow/SKILL.md" <<'EOF'
---
name: p3-allow
description: Explicit fixture skill for the Phase 3 allowed control.
---
When explicitly invoked, reply with PHASE3_ALLOW_MARKER.
EOF
cat > "$PHASE3_FIXTURE/home/.agents/skills/block/SKILL.md" <<'EOF'
---
name: p3-block
description: Explicit fixture skill for the Phase 3 blocked control.
---
When explicitly invoked, reply with PHASE3_BLOCK_MARKER.
EOF
```

优先用真实 CLI 的 skill 选择器或经当版 schema 确认的只读 skill-list 协议观察加载路径与 enabled 状态。若只能靠模型调用观察，应使用已可用的认证环境、明确调用允许/禁止 skill、保留对照和实际工具事件；模型说「我看不到」或退出 0 都不足以单独证明配置生效。需要新认证时保留门禁待验，不读取或复印真实 auth.json。

- [x] **Step 2: 完成路径矩阵**（Linux symlink；Windows/Junction 未验）

在 fixture 内记录完整展开后的 argv；至少包含以下真实对照。`CODEX_EXE` 须绑定 Step 1 的绝对路径。

```sh
(
  export HOME="$PHASE3_FIXTURE/home"
  export USERPROFILE="$PHASE3_FIXTURE/home"
  export CODEX_HOME="$PHASE3_FIXTURE/codex"
  cd "$PHASE3_FIXTURE/repo"
  "$CODEX_EXE" -c "skills.config=[{path=\"$PHASE3_FIXTURE/home/.agents/skills/block/SKILL.md\",enabled=false}]"
)
```

上例启动交互观察；非交互实验改用当版实际支持的命令。补齐空白名单、路径指向目录、文件路径、非 ASCII/空格路径、symlink 发现路径、canonical 目标路径、两个不同 ID 同 frontmatter 名、两个发现入口共享一个 canonical 目标。路径含引号或反斜杠时通过 TOML 编码器构造参数，不能照抄上例字符串插值。

Expected：文件路径规则精确命中，禁用一个同名 skill 不影响另一实际路径；canonical 规则能覆盖链接入口。偏差写出具体行为并修订计划，不手工整理成预期结论。

- [x] **Step 3: 完成 User / CLI 数组合并矩阵**（实测须显式 true，见上方偏差）

先在 fixture `config.toml` 写下列两种规则并分别实验：

```toml
[[skills.config]]
path = "/absolute/fixture/home/.agents/skills/allow/SKILL.md"
enabled = false

[[skills.config]]
name = "p3-allow"
enabled = false
```

实际文件替换为 fixture 路径。依次测试无 `-c`、`skills.config=[]`、只含 block 的新数组、allow 的 `enabled=true` 后接 block=false、相同路径重复项、CLI 规则与项目层同名字段。路径规则和 name 规则分开测，再测组合。

Expected：选中 allow 能在本次会话重新可用，block 保持不可用，fixture 配置字节不变。若数组替换不足以覆盖 User 层 deny，记录其实际叠加方式；在找到可验证的会话级方案前，不进入 Task 4，不用 CODEX_HOME 替换或改写用户文件作为产品补丁。

- [x] **Step 4: 记录 bundled 与参数追加行为的对照**（参数解析与 debug prompt-input；未调用模型）

测试 `skills.bundled.enabled=true/false`，检查已经存在和尚未创建的 `.system` 缓存；记录真实内置 skill 范围。测试普通交互、`exec` 的尾部 `-c` 是否生效，以及透传独立 `--` 后尾部 `-c` 是否退化为位置参数。测试项目 config/profile 能否改变此结论。不要把未知键被静默接受当作 bundled 生效。

- [x] **Step 5: 提交证据**

```sh
git add docs/verification.md internal/agent/codex/testdata/verification/README.md
git commit -m "docs: verify Codex skill path controls and config layering"
```

若修改 spec，显式加入该文件。只提交无秘密 fixture/记录；真实缓存、会话、认证、完整用户配置均不提交。第 9 条的最终状态需等待 Task 2 的 plugin ID 验证。

### Task 2: 验证发现目录、plugin 安装事实与配置来源


**2026-09-06 事实门禁：** 本地 Linux 53+3+3 组真实对照通过；完整证据见 verification README。无独立本地安装索引；普通 manifest namespace 并非自动可控 plugin；plugin 总开关用单个 inline table，允许 plugin 的全部路径也须 true。远端认证来源未验，Task 3已落实本地来源限定与 remote_plugin=false，不把本地门禁等同通用远端支持。bundled=true保留User单项deny。

**Files:**

- Modify: `docs/verification.md`（第 6、9 条和 bundled 补充）。
- Modify: `internal/agent/codex/testdata/verification/README.md`。
- Create: `internal/agent/codex/testdata/catalog/README.md`（当版原始 schema 的脱敏 fixture 说明）。
- Create after verification: `internal/agent/codex/testdata/catalog/` 下按真实 schema 命名的配置/安装/manifest fixtures。

- [x] **Step 1: 建立不同 cwd 与目录层级的标记 fixture**

```text
fixture/
  home/.agents/skills/user/SKILL.md
  codex/skills/legacy/SKILL.md
  repo/.git/
  repo/.agents/skills/root/SKILL.md
  repo/.codex/skills/old-root/SKILL.md
  repo/apps/.agents/skills/parent/SKILL.md
  repo/apps/api/.agents/skills/cwd/SKILL.md
  repo/apps/api/.codex/skills/old-cwd/SKILL.md
  repo/apps/web/.agents/skills/sibling/SKILL.md
  repo/apps/api/deeper/.agents/skills/descendant/SKILL.md
```

给每项不同 frontmatter `name`。在 repo、apps/api 和无 git 根目录各启动一次；增加每个 skills 根内 `group/nested/SKILL.md`、隐藏目录、链接 skill、链接中间目录、同名 root/child、不同 basename 相同 name。验证「仓库目录遍历」和「skills 根内部递归」这两个独立维度。Unix admin 根在容器或可控隔离环境验证，不向真实 `/etc/codex` 写 fixture。

- [x] **Step 2: 验证安装索引、缓存和原地来源**

使用不带 MCP、hooks、认证和网络访问的本地测试 plugin，准备 enabled、disabled、同 ID 多版本、仅配置键、仅 marketplace 候选、已卸载但缓存残留、缺失 active root 等对照。所有安装动作仅作用于 fixture CODEX_HOME。

记录实际 ID、安装索引文件名和 schema、有效安装根选择方式、manifest 文件名、skill 根默认值及自定义路径规则、原地开发 plugin 是否存在。明确缓存目录里的哪些条目表示实际安装，哪些只代表候选或历史版本；不预设 `cache/<market>/<plugin>/<version>` 是完整契约，不按版本字符串排序猜当前版本。

- [x] **Step 3: 验证配置层和插件开关**

分别在 fixture 用户层、项目层、当版支持的 system/admin 层和 profile 中声明不同 plugin ID，核对哪些会进入加载集合；受信任和未信任 workspace 分开记录。只读读取所有潜在项目 plugin 键可以保守扩大关闭集合，但不因此改写 workspace trust。

测试当前 disabled plugin 被 `-c 'plugins={"id"={enabled=true}}'` 打开、enabled plugin 被 false 关闭、无配置键的实际安装项、未安装允许项、plugin 内多个 skills、普通 skill 与 plugin skill 同名以及原地 plugin。确认 `skills.config` 是否作用于 plugin skills；产品仍按整体 plugin 控制，不顺带实现插件内部选择。

同时核对能引入新来源的参数与配置：cwd、profile、配置覆盖的父表、发现根、安装路径/marketplace 来源。形成确定的「可读入并集」或「活动隔离拒绝」清单；未支持但会加载 skill/plugin 的来源不得静默漏扫。若 bundled/plugin 开关不存在或不生效，本阶段门禁不通过。

- [x] **Step 4: 固定测试 fixture 与结论**

将实验确认的元数据结构复制为最小 fixtures，替换绝对路径为测试 token 并在测试加载时绑定 t.TempDir。README 逐个说明含义、支持版本、active 与 stale 的区别；不要写虚构的生产元数据示例。更新第 6、9 条状态，必要时标「不符（已修订 spec §x）」；第 1、6、9 条全通过后才开始 Task 3。

- [x] **Step 5: 提交证据**

```sh
git add docs/verification.md internal/agent/codex/testdata/verification/README.md internal/agent/codex/testdata/catalog
git commit -m "docs: verify Codex discovery and installed plugin metadata"
```

### Task 3: 将实验结果与设计补充写入总契约

**Files:**

- Modify: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`。
- Modify: `docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md`。

- [x] **Step 1: 核对三项门禁与本计划前提**

把「安装版本实测」「官方文档」「本项目设计选择」分开。已确认本地来源门禁通过；Windows/Junction、模型调用和认证远端未验单独列范围及后续责任，不把它们冒充本地门禁，也不阻断已验证本地契约的实现。官方固定源码说明 remote loader 和版本总序；真实 Linux 对照固定本地行为，项目明确追加远端禁用/不支持来源 fail-closed 限制，三者分开记账。

- [x] **Step 2: 修订 spec 的具体段落**

| spec 段落 | 必须写清的内容 |
|---|---|
| §3/§4.1 | 实际支持版本、原生根与递归规则、安装来源、bundled 排除根、适用配置层 |
| §4.3/§7.4 | Codex 用 ID 对应路径控制；同有效名不同路径不自动相互允许；同 canonical 别名的限制 |
| §7.2 | 跨层累积的显式 true/false 方案、空数组、plugin true/false、missing 判定；不重定向 CODEX_HOME |
| §6.2 | CLI dot splitting、TOML 值中的引号键、跨参数来源取值、cwd/profile/来源覆盖和内部 `--` 的拒绝边界 |
| §10 | Codex 配置/安装清单不完整、路径 canonicalize 失败、unsupported-source 的 fail-closed 行为 |
| §14.3/§14.5 | 本期跨目标来源范围；非目标 Claude plugin 全集在 Phase 5 接入，保留后续责任 |

根据 Task 2 结果判断是否需要读取额外配置文件或新的扫描策略，并把确切路径/字段填入 Task 4–5。不得让实施者自行选择数组语义或缓存布局。

- [x] **Step 3: 复核六个冻结类型**

本方案只新增 adapter 内部类型、Root/ScanResult 辅助信息和消费方接口。若确需改变冻结类型，先在 spec 给出完整新定义及兼容方式，再重排本计划，禁止在编码 Task 中顺带修改。

- [x] **Step 4: 检查文档差异**

```sh
git diff --check
git diff -- docs/superpowers/specs/2026-09-02-skill-scope-design.md docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md
```

Expected：未删除其他 phase 的责任，实验范围与平台限制可追溯；后续任务不再包含尚未决定的配置合并或安装结构分支。

- [x] **Step 5: 提交契约**

```sh
git add docs/superpowers/specs/2026-09-02-skill-scope-design.md docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md
git commit -m "docs: define verified Phase 3 Codex isolation contract"
```

### Task 4: 补齐 Codex 根发现和通用外来读取

**Files:**

- Create: `internal/skill/codex_scope.go`、`internal/skill/codex_scan_test.go`、`internal/skill/foreign_roots_test.go`。
- Modify: `internal/skill/scope.go`、`internal/skill/scan.go`、`internal/skill/foreign.go`。
- Create: `internal/host/skill_roots.go`、`internal/host/skill_roots_unix.go`、`internal/host/skill_roots_windows.go`、`internal/host/skill_roots_test.go`。
- Test: `internal/skill/scan_links_test.go`、`internal/skill/foreign_unix_test.go`。

- [x] **Step 1: 写目录与元数据测试**

用 `fstest.MapFS` 包装既有 FileSystem 接口，逐行验证：

| 输入 | 断言 |
|---|---|
| 缺省 / 自定义 CODEX_HOME | 全局根不重复，env.Cwd 和 env.Home 不被修改 |
| cwd 在 repo 子目录、`.git` 为目录/文件 | 仅生成已验证的项目链；Scope 相对仓库根 |
| 无 git 根、父目录有 skills | 只读 cwd 项目根 |
| sibling/descendant、skills 内 group、隐藏目录 | 与 Task 2 的真实读取范围一致 |
| admin 根注入临时目录 | LevelAdmin、SourceCodex、Names[Codex] 正确；测试不读宿主 `/etc` |
| native/foreign 合法 Codex 显式 Root | Source/Scope/Kind/PluginID/PluginAgent/Names 一致；缺 description 的 foreign 保留但无 CodexNames |
| foreign command | 原命名空间保留，可报告 command-only，不投影 |
| foreign `.claude-plugin` 目录 | 停止普通 skill 扫描，保留 Phase 2 边界 |
| `.system` 缓存 | 从普通 roots 排除，不成为普通/外来 skill |
| 缺失根 / 已进入目录 EACCES / 断链 | 分别跳过 / 报错 / 报错 |
| foreign 特殊或超限 SKILL.md | 不读无界正文；保留 ID 与 Rejection，Names 不伪造 |

- [x] **Step 2: 运行红灯**

```sh
go test ./internal/skill -run 'TestScanCodex|TestScanForeignRoots' -count=1
```

Expected：因新增方法缺失或新增 root/metadata 断言失败。不要把 fixture 自身路径错误当成需求红灯。

- [x] **Step 3: 实现最小扫描 API**

```go
// internal/skill/codex_scope.go
func (s Scanner) ScanCodex(env host.Env, paths host.CodexPaths) (ScanResult, error) {
	roots, projectRoot, err := s.CodexRoots(env, paths)
	if err != nil {
		return ScanResult{}, err
	}
	result, err := s.ScanRoots(roots)
	result.ProjectRoot = projectRoot
	return result, err
}
```

增加 `Scanner.CodexRoots(env, paths)`、`Scanner.ClaudeRoots(env)`；后者复用 `claudeScanRoots`，不另写 Claude 遍历。配置层读取需要项目链时提取 `ProjectDirectories(FileSystem, cwd) (root string, dirs []string, err error)`，让现有 Claude 与新 Codex 共用 findGitRoot/projectDirectories 的结果。

`host.ResolveCodexPaths` 的 Unix helper 返回 env.Home、admin=[/etc/codex/skills]、systemConfig=[/etc/codex/config.toml]；Windows helper 使用 KnownFolderPath，后两项为空。显式 CODEX_HOME trim 后须绝对目录，否则错误；缺省使用该平台 home/.codex。Windows API 依据固定 x/sys v0.41.0 的 go doc 与 Context7 /golang/sys 已核对。两平台都将 Home 用于 .agents 根，不能只修 admin。host 测试注入 home 查询 seam 验证失败与忽略 HOME/USERPROFILE，不读取真实目录。

`ScanForeignGlobals` 委托新的 `ScanForeignRoots`，保留旧调用兼容。抽取读取循环时复制完整 Root 元数据；foreign command 复用命名空间遍历及普通文件检查，增加同样的有界读取。Root 增加字段 `ScanMode ScanMode`；skill/scope.go 定义 `type ScanMode uint8` 及 `const (DirectChildren ScanMode = iota; CodexRecursive)`（零值保留既有行为），Codex 普通/plugin/foreign roots 显式设 CodexRecursive；Claude skill 直子项和 command 规则不变。Codex 遍历跳隐藏目录、跟随中间链接、遇 SKILL.md 继续递归；当前递归链 canonical 目录防环，保留不同 discovery 入口，不全局去重丢身份。读取 .codex-plugin/plugin.json 的 manifest.name 作为其子树名称前缀，不停止普通遍历、不设置 PluginID；plugin cache root 的 PluginID 由 Catalog 提供。禁止先 ScanRoots 无界读完再给结果加 Rejection。

增加 `internal/skill/codex_metadata.go`、`codex_metadata_test.go`，定义非冻结 `CodexManifest{Name, Skills string}` 与 `Scanner.ReadCodexManifest(pluginRoot string) (CodexManifest, error)`，安全读取 .codex-plugin/plugin.json，name 必须非空字符串，skills 缺省 skills/ 或相对字符串；缺文件保留 ENOENT 由普通扫描跳过，Catalog 将该缺失当坏 active cache。description 仅在读 SKILL 时临时校验。原生缺 frontmatter 或 description 缺失/空/非字符串 fail-closed；foreign 无 frontmatter/缺 description 保留可投影 Location、不给 Names[Codex]；非法 YAML/字段类型错误照旧失败。缺 name 回退 basename，有 namespace 则 `<manifest.name>:<name>`。更新旧 Codex 可见性测试，不给 Claude 投影强加 description；`internal/cli/integration_test.go:skillDocument` 的 name-only fixture 继续可用。测试覆盖 parent/child 双 SKILL、local namespace 非 plugin、循环有界与双别名均保留、native/foreign 的 description 差异。

- [x] **Step 4: 运行绿灯和现有扫描回归**

```sh
go test ./internal/skill ./internal/host -count=1
go test -short ./...
```

Expected：全部 PASS；Linux/macOS 跑 FIFO 和真实 symlink，Windows 跑已有 Junction 测试。字节限额、read/close 错误传播、源内容不变的 Phase 2 断言继续通过。

- [x] **Step 5: 提交**

```sh
git add internal/skill internal/host/skill_roots.go internal/host/skill_roots_unix.go internal/host/skill_roots_windows.go internal/host/skill_roots_test.go
git commit -m "feat: discover Codex roots and generalize bounded foreign scans"
```

**Task 4 实施记录（2026-09-06，生产提交 `e445bec`）：**

- 新增 CodexPaths、CodexRoots/ScanCodex、ClaudeRoots/ProjectDirectories、CodexRecursive、manifest 与 description 校验；原生和 foreign 共用遍历，foreign SKILL/command 保持有界读取与完整 Root 元数据。CLI 的外来根装配保持 Task 10 前的环境快照来源，未提前切换 Known Folder/admin。
- TDD 初始红灯：`go test ./internal/skill -run 'TestScanCodex|TestScanForeignRoots' -count=1` 因缺 CodexPaths/ScanCodex/ScanMode/ReadCodexManifest 编译失败；host 路径测试因缺 resolver seam 编译失败。新增根自身 SKILL/manifest 测试先复现遗漏根、child 无前缀，再通过；dangling command root 测试先复现被跳过，再通过。
- 补充真实 CLI 0.153.1 对照确认 skills 根自身 SKILL.md 和 child 均加载，根 manifest 为两者增加前缀。两组隔离 debug prompt 均 exit 0，未调用模型；父代理取证目录为 `/var/tmp/skope-p3-root-o3rek403/`，实现测试为 `TestScanCodexIncludesRootSkillAndRootManifestNamespace`。
- description 缺失、空值、非字符串仅使 foreign 无 CodexNames，保留可供 Claude 投影的 Location；native 同情况静态错误。既有非法 YAML/name 非字符串仍报错，冻结 Location 未加字段。固定 x/sys v0.41.0 的 FOLDERID_Profile 已为指针，正确调用是 `windows.KnownFolderPath(windows.FOLDERID_Profile, 0)`。
- Windows `go test ./internal/skill ./internal/host -count=1`、`go test -short ./...` PASS；golangci-lint v2.13.2 对两个包检查为 0 issues，并运行其 gofmt/goimports formatter。WSL 原生 Linux 两包 `go test -race ... -count=1` PASS，包含 FIFO、真实 symlink；Windows Junction 验证中间目录、双 discovery、循环有界与 scanner 重用。覆盖率抽查 skill 94.3%、Windows host 68.2%，不以该抽查代替最终跨平台门禁。
- Linux 首轮回归指出旧“断链”fixture 实际只删除 SKILL.md、链接目标目录仍存在；已分别断言合法空分组跳过和删除目标目录后的真正断链报错，SKILL.md 自身 dangling symlink 仍 fail-closed。read/close 联合错误断言继续通过。Task 4 独立 spec/quality 审查由父代理随后执行，Phase 3 尚未完成。

- Task 4 规格补审修复：移除 ReadCodexManifest 未约定的 1 MiB 上限，复用既有普通文件检查与安全 OpenRegular 读取。大于 1 MiB 的合法 manifest（长无关 description）测试先以 invalid manifest file 红灯，修复后两种 opener 路径均通过；同时验证替换成特殊文件的错误、read/close 联合错误和 JSON 正文保密。Windows skill 包测试通过，Linux manifest 与真实 FIFO 定向测试通过，局部 lint 0 issues。

- Task 4 质量补审修复：可选 manifest 仅在 Lstat 确认路径真正不存在时跳过，.codex-plugin 目录确认存在后再 Stat；已存在的 manifest 读取错误全部传播。native/foreign 的 dangling 文件/目录、open ENOENT、read/close 联合 IO+ENOENT 共 10 个场景先复现被吞后转绿。真实 Windows Junction 补测还先复现 Lstat irregular 模式漏判，改为目录存在后总是 Stat 后通过；Linux 真实文件/目录 symlink 同时覆盖。
- 被拒绝的任意 command root 条目使用 visitor 的完整相对根 ID（含 Scope/NamePrefix），Names 保持空。新增 skill.Merge 复用原去重、深复制、排序和冲突构建，launch 纯库存合并保留 Skill.ID，不再展平后重新推断；/custom/a/run.md 与 b/run.md 的 scanner/merge 错误合并均先红后绿。Windows skill/launch 全包、Linux 两包 race 通过；局部 golangci-lint v2.13.2 为 0 issues，冻结类型和 CLI 来源装配未改。

### Task 5: 只读 Codex 配置与实际安装 catalog

**Files:**

- Create: `internal/agent/codex/catalog.go`、`internal/agent/codex/config.go`、`internal/agent/codex/plugins.go`、`internal/agent/codex/errors.go`。
- Create: `internal/agent/codex/catalog_test.go`、`internal/agent/codex/config_test.go`、`internal/agent/codex/plugins_test.go`。
- Test: `internal/agent/codex/testdata/catalog/` 中 Task 2 已固定的 fixtures。

- [x] **Step 1: 写 catalog 解析矩阵**

| fixture | 断言 |
|---|---|
| 无 config、无安装目录 | 空集合，成功 |
| 配置 plugin true/false | 两者 ID 均进入 PluginIDs，均不单独证明 installed |
| 实际安装、disabled 安装、同 ID 历史版本 | installed 包含实际 ID；只产生实际 active root |
| marketplace 候选、已卸载残留 | 不冒充 installed，不扫描历史 skill |
| active 安装记录缺根/损坏/未知不可判断形状 | 类型化 inventory 错误；不返回部分成功结果 |
| 已知插件无 skills / 自定义 skills 根 | 分别空 roots / 按已验证 manifest 规则解析 |
| 项目层独有 plugin 键 | 进入关闭全集，路径按实际配置层解析 |
| config.profile / 不支持来源 | unsupported-source；不实现运行时 profile 解释器 |
| 无关 model、MCP、认证等合法键 | 不拒绝、不输出、不保存到 Snapshot |
| TOML 非法、plugins 类型错误 | 只报文件路径与静态字段类别，不带正文 sentinel |
| I/O 错误、context 取消 | errors.Is 可识别；不继续读取下一个来源 |
| 调用方修改结果 | 下次 Read 和原 fixture 数据不受影响 |

- [x] **Step 2: 运行红灯**

```sh
go test ./internal/agent/codex -run 'TestCatalog|TestReadCodexConfig|TestParseCodexPlugins' -count=1
```

Expected：新增生产 reader 尚不存在或集合/错误断言失败。

- [x] **Step 3: 实现 Catalog.Read**

Catalog 复用 skill.FileSystem 与 skill.RegularFileOpener，由 `host.OSFileSystem` 满足；在 catalog.go 定义前文 CatalogSnapshot，本任务不依赖尚未创建的 adapter.go。Read 接受解析后的 host.CodexPaths。读取 SystemConfigPaths → CodexHome/config.toml → ProjectDirectories 返回的祖先链各 .codex/config.toml；收集 plugins 表所有 ID（值必须是表，enabled 若存在必须 bool），不因 untrusted 跳过潜在项目键、不复制 trust/MCP/model 合并。config.profile 存在即拒绝；system/User 读取后先检查 project_root_markers 为缺省或精确 [".git"]，再构建项目链，每级项目配置仍检查该字段，其他值或类型 unsupported-source。marketplaces 配置不参与 installed 判定；PluginConfig.mcp_servers 是合法无关 overlay。无关字段忽略；只保存静态字段，不输出配置值。再由 plugins 键逐个解析本地 cache，绝不读取/创造 installed_plugins.json。

`CatalogSnapshot` 只返回 ID、Root、静态 warnings；PluginIDs/InstalledIDs 去重并排序，roots 按已固定的 cache 活动版本规则选择，不把排序误用为版本选择。相对 manifest skill 根基于对应 plugin 根解析，路径逃逸或不受支持的额外加载入口按 Task 3 契约拒绝。

配置范围补充依据为固定 tag 的 [config.schema.json](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core/config.schema.json)，已通过 Context7 library/docs 与该文件核对；这属于源码依据，不声称做过自定义 root-marker 实验。Task 5 添加 system/User/project root-marker 缺省、[.git]、自定义数组和错误类型矩阵；Task 9 添加该 key 与 marketplaces 的 CLI 覆盖冲突。不存在 codex_home/skills_paths 配置能力；CODEX_HOME 仅在 resolver 中解析。

本地目录固定为 `CodexHome/plugins/cache/<marketplace>/<name>/<version>`，ID 精确拆为 name@marketplace；两段须非空且是单一路径组件（拒绝 /、\、.、..、绝对路径），错误归 plugins.id。配置键无 cache 只进入 PluginIDs、InstalledIDs 不含；cache-only 不枚举；disabled 配置+合法 active cache 仍 installed；marketplace/source 不读、不用于 active。候选 version 必须目录，local 若存在优先。活动目录缺 .codex-plugin/plugin.json、文件损坏/特殊/加载根逃逸均 InventoryError，不回退。合法 manifest name 为非空字符串；skills 缺省 `skills/` 或单个相对字符串（已实测 ./custom），其他形状拒绝；相对加载根 canonical 后须留在插件根内。声明根 ENOENT 表示无 skills，已进入后 I/O 错误失败。Task 4 在 skill/codex_metadata.go 暴露非冻结 `CodexManifest{Name, Skills string}` 与 `Scanner.ReadCodexManifest(pluginRoot string) (CodexManifest, error)`，只负责安全读取 JSON、name/skills 形状与默认值；Codex 原生递归与 Task 5 Catalog 共用（Catalog 的消费接口声明 ReadCodexManifest 并注入 scanner）。路径范围校验由 Catalog 完成，不让 skill import adapter。Catalog 将 manifest name 和 plugin ID 放 Root.NamePrefix/PluginID/PluginAgent。

新增 `internal/agent/codex/versions.go`、`versions_test.go`，以 [固定 store.rs](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core-plugins/src/store.rs) 和相邻 verification README 为依据。本包小型完整比较器实现 Rust semver::Version 总序：主/次/补丁数字、prerelease（数字段按数值、数字低于文本、短前缀低于长、release 最高）、build metadata（空最低，数字低于文本；数字去前导零后比长度、字典序，数值相同再比原长度；文本 ASCII 字典序，公共前缀后段数多者大）；双方都合法 SemVer 才用此规则，否则 UTF-8 字符串比较。版本解析要求严格三个 core 数字（无空白、禁止前导零、0..u64::MAX）；pre 数字段禁止前导零但无需转整数（长度比较），标识符只允许非空 ASCII 字母/数字/连字符；build 数字允许前导零；拒绝其他非法标识符，不使用忽略 build 的 semver.Compare，不引入依赖。用逐候选比较确认唯一最高（最高必须大于其余每项），混合比较有环/无唯一最高时 fail-closed，不依赖不传递比较器排序。测试 local；10>9；aaa>10；alpha.10>alpha.2；release>prerelease；3.0.0+10>+2>无build；build 的 0<00<1<01<001<2<02<002<10、数字<文本、公共前缀段数；core/pre 前导零、core 溢出、非法semver转字符串；混合环；空最高目录缺 manifest 不回退；mtime/localVersion/source.version 不影响。

Codex Cargo.lock 固定 semver 1.0.27；总序细节以 [impls.rs](https://github.com/dtolnay/semver/blob/1.0.27/src/impls.rs#L103) 与 [Version derive Ord](https://github.com/dtolnay/semver/blob/1.0.27/src/lib.rs#L154) 为准，已通过 Context7 library/docs 后直接核对固定源码；不采用生成文档中与源码相反的前导零例子。

对受管理字段做形状校验但不复刻 skill 规则合并：skills 若存在必须表，config 若存在必须表数组，各项 path/name 若存在须字符串、enabled 若存在须 bool；bundled 若存在须表，其 enabled 若存在须 bool。这些规则只校验，不存入 CatalogSnapshot 或重新应用 User deny；实际覆盖由 Task 8 全集 path true/false 完成。测试 malformed skills/config/bundled 不输出原值，合法无关字段继续接受。

语法错误包装为本包 `InventoryError{Path, Field, Cause}`。`Error()` 只格式化 path/静态字段与错误类别；`Unwrap()` 保留原始错误供 errors.Is/As 使用，不能 `%v` 原样输出可能带 TOML 值的 decoder 错误。插件文件读取先检查普通文件类型，防止 FIFO 阻塞；复用 host.OpenRegular，不新增后台进程。

- [x] **Step 4: 运行绿灯**

```sh
go test ./internal/agent/codex -count=1
go test -short ./...
```

Expected：全部 PASS；未知字段不阻塞，无真实配置读写，无真实 agent/网络调用，输出不含 fixture 中的秘密 sentinel。

- [x] **Step 5: 提交**

```sh
git add internal/agent/codex
git commit -m "feat: read Codex plugin configuration and installed sources"
```

Task 5 实施记录（2026-09-06）：新增只读 Catalog 与消费接口、静态 InventoryError、受管 TOML 形状校验、本地 active cache 与完整版本比较器；复用 Task 2 catalog fixtures，未新增依赖或修改冻结类型。旧合法 profiles 表按无关字段忽略，只拒绝单数 profile，未读取运行时 profile 文件。固定 store.rs 的版本目录过滤仅接收 ASCII 字母、数字、- _ . +，不跟随 version symlink；其他名称忽略，合法目录名的非 semver 版本才使用字符串比较。

红灯证据：首轮 `go test ./internal/agent/codex -run 'TestCatalog|TestReadCodexConfig|TestParseCodexPlugins' -count=1` 因生产包缺失失败。后续独立回归先复现 OpenRegular 后取消仍 Read 一次、Git 祖先查询取消后仍调用 9 次 Stat、断链 skills 祖先被当作无 skills、缺失 skills 路径经逃逸 symlink 被接受、非法中文版本目录劫持 active，随后最小修复并转绿。

绿灯证据：`go test ./internal/agent/codex -count=1`、`go test -short ./...` 通过；固定 golangci-lint v2.13.2 `fmt ./internal/agent/codex/...` 执行 gofmt/goimports，`run ./internal/agent/codex/...` 为 0 issues；WSL Ubuntu 离线 Go 工具链 `go test -race -cover ./internal/agent/codex -count=1` 通过，包覆盖率 87.1%，包含 FIFO 拒绝、断链/逃逸及 version symlink 回归。所有来源来自注入临时目录；没有读取真实用户配置、运行真实 Codex 或调用网络服务。ManifestReader 的单次调用无 context 参数，Catalog 在调用前后检查取消；文件操作不启动后台进程，已进入的同步 OS 调用不承诺强行中断。

补充：插件 ID 按固定 [plugin_id.rs](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/plugin/src/plugin_id.rs#L45) 校验，name 仅 ASCII 字母、数字、- _ .，点不得首尾或连续；marketplace 仅 ASCII 字母、数字、- _。空格、Unicode、marketplace 点号等先红灯复现再拒绝，错误只显示静态 plugins.id 字段。

### Task 6: Codex adapter 与原生 inventory

**Files:**

- Create: `internal/agent/codex/adapter.go`、`internal/agent/codex/inventory.go`、`internal/agent/codex/inventory_test.go`。

- [x] **Step 1: 写 inventory 行为测试**

使用 fake scanner/catalog 验证：Name=Codex；Capabilities 为 false/true/true；普通根与 plugin 根合并；同 ID 多入口保留；plugin 有 PluginAgentCodex 和 PluginID；SkillNames 只含真实 Codex 可加载名称；缺失允许插件只告警一次；配置有键但未安装仍告警；已安装 disabled 项不报 missing；Options 和输出不与输入共享 map/slice。

Catalog/scanner/context 任一失败则返回错误和空 inventory；不执行子进程。构造 New 本身不读取依赖，help/version 因此可安全注册。

- [x] **Step 2: 运行红灯**

```sh
go test ./internal/agent/codex -run 'TestCodexInventory|TestCodexCapabilities|TestCodexOptions' -count=1
```

- [x] **Step 3: 实现 inventory**

按前文接口构造 Adapter，复制 Plugins；保存 ResolvePaths 函数。Inventory 先 ResolvePaths（失败不继续），再 Catalog.Read(ctx, env, paths) 校验 root-marker，再 ScanCodex(env, paths)，再扫描已验证 plugin SkillRoots；每个外部步骤之间检查 ctx。使用 `skill.Merge` 合并扫描器 Skill，保留既有 ID 并生成 collisions；PluginIDs 来自 Snapshot，allowed-only ID 留给 Plan 的并集，不伪造 installed 状态。

读取依赖不全时返回静态配置错误；不把 executable 或选择信息塞进 Env。此 Task 不注册到 CLI；Plan 在 Task 8 实现后再添加 `var _ agent.Adapter = Adapter{}`，避免中间提交因缺少 Plan 无法编译。

- [x] **Step 4: 运行绿灯**

```sh
go test ./internal/agent/codex ./internal/agent/claude ./internal/skill -count=1
```

Expected：全部 PASS；无跨 adapter import，六个冻结类型无 diff。

- [x] **Step 5: 提交**

```sh
git add internal/agent/codex/adapter.go internal/agent/codex/inventory.go internal/agent/codex/inventory_test.go
git commit -m "feat: build Codex native inventory with plugin visibility"
```

Task 6 实施证据：定向测试先因缺少 codex.New/Options 编译失败；实现后 Windows 三包测试和全仓 go test -short ./... 通过。固定 golangci-lint v2.13.2 对新增三文件执行 gofmt/goimports，Codex 包 lint 为 0 issues；WSL Ubuntu 离线 go test -race ./internal/agent/codex -count=1 通过。覆盖逐阶段错误/取消空输出、依赖顺序、每个缺失依赖与非法插件 ID 的静态拒绝、原生/plugin 同 ID 合并、真实 Names 和 missing 告警、Options 与依赖输出切片/map 隔离。New 不访问依赖；Canonicalizer 仅保存留待 Task 8。仅使用 fake 依赖，不读取真实配置或执行 Codex。

### Task 7: 不支持投影时的原因与 canonical 别名告警

**Files:**

- Modify: `internal/skill/resolve.go`、`internal/skill/resolve_test.go`。
- Modify: `internal/agent/codex/inventory.go`、`internal/agent/codex/inventory_test.go`。
- Test: `internal/launch/projection_test.go`。

- [x] **Step 1: 写分类和无检查调用测试**

| 选中 ID 的入口 | Codex 结果 |
|---|---|
| 普通 Codex native + 任意 foreign/plugin | native，Resolved 保留名称供摘要，Plan 依 ID 回查全部普通路径 |
| 只有允许的 Codex plugin | native |
| 只有禁止的 Codex plugin | unavailable / plugin-disabled |
| 只有其他 agent plugin | unavailable / plugin-only |
| 只有 legacy command | unavailable / command-only |
| 有普通 foreign，可同时有 plugin/command | unavailable / projection-unsupported |
| 全集中不存在 | missing |

混合且无普通 foreign 的排除原因顺序固定为目标 plugin-disabled、其他 plugin-only、command-only，给出用户可操作的最直接原因。所有 `Projection=false` 用 panic trap 证明不调用 ProjectionCheck/Inspector/Copier。Claude `Projection=true` 的原选择优先级、拒绝原因、候选回退保持不变。

别名 fixture 覆盖普通同 ID 同目标、普通不同 ID 同目标、同名不同目标、普通/plugin 同 ID 同目标、不同 PluginID 下同 ID 同目标，以及一个允许一个禁止的组合。告警按控制来源区分：普通入口的键为 (ordinary, skill.ID)，plugin 入口的键为 (plugin, PluginID)。同一 canonical 路径包含多个控制来源键时，由 Inventory 生成静态共享控制告警；同一普通 ID 的重复别名或同一 PluginID 内多个 skill ID 共用路径不新增该告警。已有 different-content/effective-name collision 不替代此告警。

- [x] **Step 2: 运行红灯**

```sh
go test ./internal/skill -run 'TestResolveCodex' -count=1
go test ./internal/agent/codex -run 'TestCodexInventoryCanonicalAliases' -count=1
```

Expected：现有提前返回使 plugin/command 原因断言失败；别名告警缺失。

- [x] **Step 3: 最小修改原因分支**

先保持 native 优先，再在 `!opts.Projection` 分支检查候选类别并返回固定原因；不进入投影检查。Codex Inventory 按 RealPath 分组，对组内 (ordinary, skill.ID) / (plugin, PluginID) 控制来源键去重；超过一个键则在既有 Inventory.Warnings 中生成一次静态告警，说明这些来源共享路径开关、任一允许时该路径允许。告警不依赖本次是否已发生 true/false 冲突，因此无需向 Inventory 传入完整 skill 选择，也不声称修改 ID。测试输出稳定、不同 PluginID 同 ID 不漏警、同一控制键不重复警；Task 8 再复核 canonical 目标。

- [x] **Step 4: 运行绿灯与 Claude 回归**

```sh
go test ./internal/skill ./internal/agent/codex ./internal/launch ./internal/agent/claude -count=1
```

Expected：全部 PASS；四状态计数仍按选中 ID，不按 location 数。

- [x] **Step 5: 提交**

```sh
git add internal/skill/resolve.go internal/skill/resolve_test.go internal/agent/codex/inventory.go internal/agent/codex/inventory_test.go internal/launch/projection_test.go
git commit -m "fix: explain Codex unavailable skills and shared path controls"
```

Task 7 实施证据（2026-09-06）：两条定向红灯命令分别出现 5 个原因断言失败和 3 个共享控制告警断言失败，确认旧行为统一返回 projection-unsupported 且未生成共享路径告警。最小实现后两条命令通过；Windows 四包回归 `go test ./internal/skill ./internal/agent/codex ./internal/launch ./internal/agent/claude -count=1`、全仓 `go test -short ./...` 通过。固定 golangci-lint v2.13.2 对五个修改 Go 文件执行 gofmt/goimports，四包 lint 为 0 issues；首次 lint 检出测试变量遮蔽内置 real，改名 canonical 后通过。WSL Ubuntu 离线 `go test -race ./internal/skill ./internal/agent/codex ./internal/launch -count=1` 通过。Projection=false 分类表使用 panic checker；launch 活动执行与 dry-run 使用 panic Inspector/Copier 覆盖 native、allowed/disabled plugin、其他 plugin、command、foreign、missing，投影数和文件数为零。launch fixture 初次误将重复 ID 写进 TOML，被现有配置验证拒绝后修正；重复选择去重由 Resolver 测试覆盖。别名矩阵覆盖同控制来源去重、普通/plugin 不同控制来源、空路径、外来来源排除、路径与来源稳定排序；原 IDs、collisions 保持。只使用 fake 依赖与元数据，不读取真实配置、不执行 Codex、不重新 canonicalize，Task 8 的 Plan 尚未实现。
### Task 8: 生成 TOML 路径全集、plugin、bundled 与 remote 参数

**Files:**

- Create: `internal/agent/codex/plan.go`、`internal/agent/codex/paths.go`、`internal/agent/codex/plan_test.go`、`internal/agent/codex/paths_test.go`。
- Create: `internal/agent/codex/testdata/control-args.golden.json`、`internal/agent/codex/testdata/empty-control-args.golden.json`。
- Modify: `.gitattributes`（Codex golden 固定 LF）。

- [x] **Step 1: 写控制参数矩阵**

选择 `allow`，设置 bundled=false，允许 `keep@market`、`missing@market`，安装/配置另有 `block@market`。fixture 含普通 allow/block 与 keep 插件 skill（`/fixture/keep/SKILL.md`）时基准 argv 为：

```json
[
  "-c",
  "skills.config=[{path=\"/fixture/allow/SKILL.md\",enabled=true},{path=\"/fixture/block/SKILL.md\",enabled=false},{path=\"/fixture/keep/SKILL.md\",enabled=true}]",
  "-c",
  "skills.bundled.enabled=false",
  "-c",
  "plugins={\"block@market\"={enabled=false},\"keep@market\"={enabled=true},\"missing@market\"={enabled=true}}",
  "-c",
  "features.remote_plugin=false"
]
```

该 golden 是已验证方案的产品格式；全选仍输出全部 true，只有空 inventory 输出 skills.config=[]。同 ID 多 native 路径全部允许；不同 ID 同名只允许选中路径；same canonical OR 允许；允许 plugin 的全部路径 true、禁用 plugin 的路径 false（即使技能 ID 被选中），plugin 总开关独立；unavailable/missing 不凭空生成路径；输入顺序变化输出稳定。空 plugin 并集仍写 plugins={}，四对 -c 固定存在。bundled 系统路径不进入路径数组，remote_plugin 固定 false。

路径用真实 temp 文件验证绝对与 symlink/Junction 解析，再以 fake canonicalizer 做跨平台 golden。测试引号、反斜杠、中文、空格、制表/换行、非 BMP 字符；每个 `-c` 值用 go-toml 解码回结构后与原值比较，不能只断言字符串含反斜杠。Windows 路径和 Unix 路径分开，不把 Unix 绝对路径交给 Windows filepath 当真实路径。

- [x] **Step 2: 运行红灯**

```sh
go test ./internal/agent/codex -run 'TestCodexPlan|TestCanonical' -count=1
```

- [x] **Step 3: 实现路径集合与 TOML 编码**

允许集合来自选中 native ID 的普通 Codex location；对 inventory 中每一个普通与 plugin Codex location 调用 canonicalizer，包括选中项；plugin path 的 allowed 直接来自 Options.Plugins，不依赖 Resolved skill 选择。将结果转换为本平台绝对路径；空路径、非绝对路径、canonicalize 失败、解析后与 inventory.RealPath 指向不同目标时 fail-closed，避免扫描后链接改向造成误禁用。保留实际大小写，按最终路径字符串稳定排序。

每条 canonical 路径只记录一个 `allowed bool`，通过 OR 合并各发现入口；最终所有路径输出一次 true/false。普通/plugin 及不同 PluginID 的物理别名共同 OR，冲突允许优先；共享控制告警由 Task 7 预先写入 Inventory.Warnings，并沿既有摘要链路输出。Plan 只做 OR 和 canonical 复核，不新增告警出口、不修改 inv/resolved、不扩展冻结 LaunchPlan；复核发现链接改向仍 fail-closed，不在 Plan 临时补警或继续运行。测试一个允许一个禁止时数组只有 true、既有告警可见且 Plan 不改变输入；不依赖重复规则或顺序覆盖。

path 值和 plugins inline table 的键共用只输出 TOML basic quoted string 的小函数。不用 Go `strconv.Quote`，因为它可能输出 TOML 不接受的 `\xNN`、`\a`、`\v`；不用先序列化整份配置再从字符串中截取值。编码函数如下，测试用已固定版本 go-toml 进行语义往返：

```go
func tomlString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("TOML string contains invalid UTF-8")
	}
	var encoded strings.Builder
	encoded.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			encoded.WriteByte('\\')
			encoded.WriteRune(r)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&encoded, "\\u%04X", r)
			} else {
				encoded.WriteRune(r)
			}
		}
	}
	encoded.WriteByte('"')
	return encoded.String(), nil
}
```

用 `toml.Unmarshal` 验证生成键只有 plugins → 原 ID → enabled 三层；ID 中 `.`、`@`、引号不能拆成额外配置层。路径与 ID 的非法 UTF-8 返回静态错误。无需另一套引号函数，也不改变 argv 之外的任何源字节。

Plan 的输出顺序固定为 skills.config、bundled、一个 plugins inline table（内部 ID 排序）、features.remote_plugin=false。PluginIDs 与 Options.Plugins 取并集，所有项都写 bool；生成结果不变更 inv/resolved/options。`Files`/`Env` 为空，`sess=nil` 也可以规划；不把原 config.toml 放入 LaunchPlan 或 dry-run。

增加 `var _ agent.Adapter = Adapter{}`。所有路径唯一；用真实 User deny 对照保持 path=true 覆盖语义，不实现数组替换假设。

- [x] **Step 4: 运行绿灯并核对 golden**

```sh
go test ./internal/agent/codex ./internal/agent/claude -count=1
git diff --check
```

Expected：argv golden、TOML 往返、路径改向错误均 PASS；`Plan.Files` 长度为 0，环境无 CODEX_HOME 改写。拒绝更改源目录或创建配置文件来让测试通过。

- [x] **Step 5: 提交**

```sh
git add internal/agent/codex/plan.go internal/agent/codex/paths.go internal/agent/codex/plan_test.go internal/agent/codex/paths_test.go internal/agent/codex/testdata/control-args.golden.json internal/agent/codex/testdata/empty-control-args.golden.json .gitattributes
git commit -m "feat: generate Codex path and plugin isolation overrides"
```

Task 8 实施证据（2026-09-06）：定向测试先确认缺少 Plan，空桩随后触发参数矩阵、非法插件 ID、canonical 路径校验和 TOML 往返行为失败。最小实现后 Windows Codex/Claude 回归、全仓 go test -short ./... 通过；固定 golangci-lint v2.13.2 对新增/修改 Go 文件执行 gofmt/goimports，Codex 包 lint 为 0 issues。首次 lint 检出 real 遮蔽内置函数，改名 canonical 后通过。WSL Ubuntu 离线 go test -race ./internal/agent/codex -count=1 通过。真实临时文件及 Windows junction/Linux symlink 覆盖 canonical 别名折叠、禁用入口改向和删除失败；生产 Scanner→Inventory→Plan fixture 证明 .system 不进入路径数组且 config.toml 字节不变。golden 仅在断言前将本机绝对临时根替换为 /fixture，不弱化生产 filepath.IsAbs。覆盖同 ID 多路径、同名不同 ID、普通/多插件共享路径 OR、禁用插件不随技能 ID 开启、missing/unavailable 不发明路径、固定四对参数、稳定排序、输入不变、nil session 与空 Files/Env。路径及任意字符串键使用已固定 go-toml 语义往返，包含 Unicode、引号、反斜杠、控制字符及非法 UTF-8；生产插件 ID 仍严格校验。export_test.go 仅追加私有编码函数的测试别名。未执行真实 Codex、未读取真实用户配置；最终 handoff 和真实 skope 验收仍属后续任务。

### Task 9: Codex 冲突参数检测与值保密

**Files:**

- Modify: `internal/cli/conflict.go`、`internal/cli/conflict_test.go`。
- Create: `internal/cli/codex_conflict_test.go`。
- Test: `internal/launch/launch_test.go`、`internal/cli/args_test.go`。

- [x] **Step 1: 写带来源的参数矩阵**

逐个覆盖 config 与 command-line 来源：`-c value`、`-c=value`、`--config value`、`--config=value`，以及 `-cvalue`。保护 skills/plugins 的父表和后代：`skills={...}`、`skills.config=...`、`plugins={...}`、`plugins."p@m".enabled=...`；按实际 CLI 的键解析语义覆盖引号、空白和近似名字。

`skills_extra`、`my.skills`、`model`、`model_reasoning_effort` 等无关键不冲突。引号在当版 CLI 中是原始键字符，不是 TOML 路径分隔符；外层受保护父键仍拒绝，不按 TOML 引号去包裹后误判断键。

完整保护集与解析规则遵守 spec §6.2：CLI dot splitting 不解 TOML 引号；skills/plugins/features 父表、remote_plugin、profile/cwd、project_root_markers、marketplaces 必须覆盖。测试 `--enable remote_plugin`、`--enable=remote_plugin`、`--disable remote_plugin`、`--disable=remote_plugin` 及其他 feature 通过；Task 9 用固定 CLI --help/解析对照确认实际支持形式，不因未知 flag 被接受而称能力已验。参数值内出现 --cd 文本不当作 flag，值中 = 只切首个。

增加 configArgs 末尾 `-c`、userArgs 首个 `skills.config=...` 的跨边界取值；值中的 `=` 不影响 key 提取；缺失/空 value 返回只含 flag/source 的参数错误。覆盖参数值里出现 `--cd`/`--profile` 文本的正常 prompt，不把单个字符串内部词语当 flag。

按 Task 3 约定测试 cwd/profile/来源覆盖与内部独立 `--` 的拒绝；`none` 在 Service 中跳过 checker。所有错误插入 SECRET_SENTINEL 证明值和原始 parser 错误均不泄漏。

- [x] **Step 2: 运行红灯**

```sh
go test ./internal/cli -run 'TestCheckConflicts.*Codex|TestCodexConflict' -count=1
```

Expected：当前 Codex 分支直接成功，受保护参数测试失败。

- [x] **Step 3: 实现 token 提取和受保护键识别**

先拼接两组参数，同时为每个 token 保留来源；不能分别检查两组后丢掉跨边界的 flag/value 关系。返回冲突时 Source 取 flag token 的来源，跨边界时新增可选 ValueSource 保存值来源（只输出来源标签，不保存/输出值）；同来源及 Claude 原错误文案不变。维护真实 CLI 支持的取值形式；消费配置值后不再把该值当 flag 扫第二遍。

对配置赋值按首个 = 提取 key，整体 trim 后 dot splitting，内部各段不 trim；不做 TOML quoted-key 解析，不解析/输出原 RHS。检查 skills/plugins/features 父表与 remote_plugin、profile/profiles、project_root_markers、marketplaces 受保护子树；不使用仅匹配 skills. 的字符串前缀。

`ConflictError` 增加 `Agent skill.Agent`，Error 使用目标名称，Flag 保留 `-c`/`--config` 或冲突 flag，不加入值。现有 Claude 输出保持原样。launch 仍只调用注入的函数；none、首次未知参数停止 skope 解析、原 argv 字节不变等已有行为保持。

- [x] **Step 4: 运行绿灯**

```sh
go test ./internal/cli -run 'TestCheckConflicts|TestCodexConflict|TestParseLaunchArgs' -count=1
go test ./internal/launch -count=1
```

Expected：两种 agent 冲突矩阵通过；冲突在 factory/inventory/staging/handoff 前阻断；错误无敏感值。

- [x] **Step 5: 提交**

```sh
git add internal/cli/conflict.go internal/cli/conflict_test.go internal/cli/codex_conflict_test.go internal/launch/launch_test.go internal/cli/args_test.go
git commit -m "feat: reject conflicting Codex configuration and source arguments"
```

**2026-09-06 Task 9 实施记录：** Codex checker 拼接 config/user tokens 并保留来源，覆盖 5 种 config 形式、cwd/profile、remote_plugin feature 开关和 agent argv 独立分隔符；整体 key trim 后直接 dot splitting，不 trim 内部分段、不解析引号或 RHS。ConflictError 增加 Agent/可选 ValueSource，Claude 旧文案保留；无效值与跨来源错误只输出静态说明、flag 和来源。真实 CLI 20 组 features 对照确认空白、引号和 --enable 顺序覆盖，10 组 runtime prompt-input 确认 cwd/profile 参数形式，复现脚本和断言见 verification/cli_forms.py 与 README。CLI 黑盒、Service 提前阻断/none、skope 首分隔符回归覆盖；Service 未新增 cli 依赖，未注册 Codex 或改 foreign 装配。

验证：首先定向 Codex 测试因原分支返回 nil 而红灯；实现后定向 cli 测试、launch 全包、Windows 全仓 short 通过。跨来源无效值缺少 ValueSource 的补充回归先红后绿。Linux cli/launch race 通过；golangci-lint v2.13.2 fmt --diff 无输出、两个包 run 为 0 issues。等待独立 spec 与 quality 审查。

### Task 10: 按目标合并外来来源与 launch 编排

**Files:**

- Create: `internal/cli/sources.go`、`internal/cli/sources_test.go`、`internal/launch/codex_test.go`。
- Modify: `internal/launch/launch.go`、`internal/launch/inventory.go`、`internal/launch/inventory_test.go`、`internal/launch/orchestration_test.go`。
- Modify: `internal/skill/scan.go`（ScanResult.Warnings）、`internal/cli/root.go`（Foreign 装配）。
- Update test doubles: `internal/launch/launch_test.go`、`internal/cli/phase2_application_test.go`。
- Modify: `internal/cli/integration_test.go`（公共 integrationFixture.run 的 binary 防护）、`internal/cli/phase2_integration_test.go`（runPhaseTwo 及直接公共 fixture 调用的回归断言）；后续 phase3 binary fixture 必须复用该入口。

- [ ] **Step 1: 先完成公共 binary fixture 防护，再写 scanner 到 Service 的测试**

此步必须在 Step 3 将 Codex Foreign 接入生产 composer 之前完成，并先于本 Task 的任何 binary 回归运行；不得延到 Task 12。`internal/cli/integration_test.go:integrationFixture.run` 在启动真实 skope 子进程前检查本次命令：Windows 的 active（含 dry-run）直接 skip，说明 HOME/USERPROFILE 不重定向 Known Folder；none/help 仍运行。不要在 newIntegrationFixture 构造时无条件 skip，Phase 2/3 注入应用测试仍须复用临时 fixture 并执行。

Unix 的 active binary 运行前用 Lstat 检查固定 `/etc/codex/config.toml`、`/etc/codex/skills`；两者均 ENOENT 才启动。任一存在（包括链接）则 skip 并提示需要隔离环境，其他 Lstat 错误使测试失败；不打开宿主配置或遍历 skill 内容。该保护同时覆盖 Claude active，因为新 foreign composer 也会读取这些 Codex 来源。核对 `phase2_integration_test.go:runPhaseTwo` 委托公共 run，所有直接 fixture.run 调用都受保护；新增绕过公共入口的 binary 测试必须先接入同一 guard。

guard 判断用注入 Lstat/平台输入测试 Windows active、Windows none/help、Unix 两根缺失/存在/错误，不接触真实来源正文。`phaseTwoService` 与本 Task 新增应用测试在运行前注入临时 Home/CodexHome/AdminSkillRoots/SystemConfigPaths，不能调用生产 Known Folder resolver。随后运行下表测试；Step 4 回归必须确认 guard 已生效。


| 目标 / fixture | 断言 |
|---|---|
| Codex + Claude-only skill | unavailable，不误报 missing，不 Inspect/Copy |
| Codex + Claude command | unavailable / command-only，保留命名空间 |
| Codex + 同 ID 普通 native 和 foreign | native；denylist 只涉及 Codex 原生路径 |
| Claude + Codex 新项目/admin 来源 | 合格项可投影，Scope/ID 不丢失 |
| Claude + Codex plugin-only | unavailable / plugin-only，绝不复制 plugin |
| Claude + Codex bundled 缓存 | 不把缓存当 foreign skill |
| foreign 特殊/超限/I/O 错误 | 前两者保留 Rejection，I/O fail-closed；沿用 Phase 2 语义 |
| catalog warning、merged collision | 与原生 warning 一起保留，无可变别名 |
| Codex active | Stage → Plan → Write(empty) → Publish → Report → Handoff，只有 owner.json |
| Codex dry-run | Preview，无 Stage/Write/Publish/Handoff；Plan 不创建文件 |
| Codex none | 不调用 factory/foreign/catalog/Inspector，坏 skillsets/agent 元数据旁路 |
| Plan canonicalize 失败、Report/Handoff 失败 | 既有 abort 和 joined cleanup error 语义保持 |

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/launch -run 'TestServiceCodex|TestMergeForeign' -count=1
go test ./internal/cli -run 'TestBinarySourceGuard|TestForeignSources' -count=1
```

- [ ] **Step 3: 实现消费方接口与 CLI composer**

将 ForeignScanner 替换为前文 `ScanForeign(ctx, env, target, maxBytes)`，Service 原位置传 req.Agent 和已有 `projection.MaxBytes`，更新所有 fake。实现 `cli.foreignSources`：

1. target=Codex：取 Scanner.ClaudeRoots → ScanForeignRoots，保留 command；不调用 claude.Adapter.Inventory 或 proc。
2. target=Claude：先 resolvePaths、Catalog.Read 校验配置/根边界，再取 Scanner.CodexRoots 与 Catalog 的有效 plugin roots → ScanForeignRoots；带完整 PluginAgent/PluginID，bundled 根排除。
3. 其他 target：返回静态 unsupported-target 错误，等待 Phase 4 接入。

提供 `cli.NewForeignScanner(scanner skill.Scanner, catalog codex.SourceReader, resolvePaths func(host.Env) (host.CodexPaths, error)) launch.ForeignScanner` 供生产和黑盒应用测试共同装配；返回私有 composer，保存 resolver，构造不读取依赖；仅 target=Claude 调用 resolver 一次并将同一 paths 传 CodexRoots/Catalog.Read。Catalog 对外来目标的静态 warnings 放入非冻结 ScanResult.Warnings；`mergeForeignInventory` 复制原生和 foreign warnings，保持原生 PluginIDs/SkillNames 的归属不变。不能将 Codex PluginIDs 混进 Claude 的开关全集。

Codex 自己的普通根在原生扫描中只读一次；外来扫描不再重复 `.agents/skills`/CODEX_HOME。构造两个 adapter 本身无副作用，Codex 启动不要求机器安装 Claude。

- [ ] **Step 4: 运行绿灯与 Phase 2 全链路回归**

```sh
go test ./internal/launch -count=1
go test ./internal/cli -run 'TestBinarySourceGuard|TestForeignSources|TestIntegrationPhaseTwo' -count=1
go test -short ./...
```

Expected：可运行测试全部 PASS；Windows active binary 按 Step 1 在启动前 skip，none/help 与注入应用测试运行；Unix 仅在固定系统来源均不存在时运行 active binary，并记录实际执行/skip。原生元数据不会被空 Names 的 foreign rejection 覆盖；测试环境新增的 Codex Home/CodexHome/配置/admin 根全部由 resolver 指向 fixture；Claude 的 name-only foreign fixture 仍可投影。

- [ ] **Step 5: 提交**

```sh
git add internal/cli/sources.go internal/cli/sources_test.go internal/cli/root.go internal/cli/phase2_application_test.go internal/cli/integration_test.go internal/cli/phase2_integration_test.go internal/launch internal/skill/scan.go
git commit -m "feat: merge target-specific foreign sources during launch"
```

### Task 11: 注册 Codex 命令并输出真实计划摘要

**Files:**

- Modify: `internal/cli/root.go`、`internal/cli/root_test.go`、`internal/cli/completion_test.go`、`internal/cli/render.go`、`internal/cli/render_test.go`。
- Create: `internal/cli/codex_application_test.go`。
- Create: `internal/cli/testdata/codex-summary.golden.txt`、`internal/cli/testdata/codex-dry-run.golden.txt`。

- [ ] **Step 1: 写命令与输出测试**

`skope --help` 列出 codex；`skope help codex` 返回帮助且不 Snapshot/read/probe；`skope codex -s none -- --help` 原样进入 agent；无 set 保持现有错误。completion 增加 codex，不引入 TTY 选择器。

完整 fixture 预期包含：

```text
→ Skillset [dev] for codex
  skills: 2 native, 0 projected, 2 unavailable, 1 missing
           unavailable: claude-only (目标 agent 不支持投影)
           unavailable: plugin-only (所属 Codex plugin 未在 plugins.codex 中允许)
           missing: absent
  plugins: 1 allowed, 2 disabled
  bundled: off
```

计数必须从同一真实 Codex Adapter.Plan 交叉核对：plugins 行等于实际写入 true/false 的数量；不复用带 Claude 依赖提示的整行。dry-run 断言 Files 仅 owner.json、无生成 settings 文件和投影正文、无继承环境或用户 config 内容，argv 中 `-c` 的 TOML 内容完整显示且只在展示时转义。

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/cli -run 'TestCodexApplication|TestCodexOutput|TestRoot|TestCompletion' -count=1
```

- [ ] **Step 3: 装配入口和渲染**

root 添加 `newLaunchCmd(skill.AgentCodex, runLaunch)`。NewRegistry 显式注册 claude.New 和 codex.New；claude.Options 只接 selection.Plugins["claude"]，codex.Options 只接 ["codex"]，Bundled 共用并集结果。未调用的 adapter 不进行任何读文件/探测。

reportResolution 从 result.Resolved.Agent 获取目标；只有 Claude 输出现有依赖说明。`unavailableReason` 接受目标，plugin-disabled 展示对应 `plugins.<agent>`，所有外部文本继续通过 termsafe。Codex 同有效名 collision 只报告同名事实，不能套用「允许其一就允许两者」的错误解释。

保留 summarizePlugins 的集合算法；用真实 Plan 解码后的插件数量测试它，而不另设不同计数逻辑。根帮助与 version 的安全输出、writer 失败传播、隐藏 completion 控制字符拒绝均沿用原路径。

- [ ] **Step 4: 运行绿灯和 CLI 回归**

```sh
go test ./internal/cli -count=1
go run ./cmd/skope --help
go run ./cmd/skope help codex
```

Expected：CLI tests 全 PASS，帮助出现 Codex；两条帮助命令不读取真实 agent 配置、不启动 agent。不运行未隔离真实用户来源的 active dry-run 作测试。

- [ ] **Step 5: 提交**

```sh
git add internal/cli/root.go internal/cli/root_test.go internal/cli/completion_test.go internal/cli/render.go internal/cli/render_test.go internal/cli/codex_application_test.go internal/cli/testdata/codex-summary.golden.txt internal/cli/testdata/codex-dry-run.golden.txt
git commit -m "feat: expose Codex launches and agent-specific previews"
```

### Task 12: fake agent 端到端、输出保密与回归矩阵

**Files:**

- Create: `internal/cli/phase3_integration_test.go`、`internal/cli/phase3_application_test.go`。
- Test: `internal/cli/phase2_integration_test.go`、`internal/cli/projection_test.go`、`internal/host/fs_windows_test.go`。
- Test: `internal/cli/integration_test.go`（复用 Task 10 已交付的公共 binary guard，不在此 Task 首次补防护）。
- Modify only if needed: 对应失败的业务文件，按失败用例限定修改。

- [ ] **Step 1: 建立独立 fixture 与 fake-agent 启动链**

复用 `testutil.BuildFakeAgent`、`BuildSkope`。完整测试在 `testing.Short()` 时跳过编译 helper；纯应用注入测试保持短测试可执行。每个 fixture 显式设置 HOME/USERPROFILE、CODEX_HOME、CLAUDE_CONFIG_DIR、SKOPE_HOME 和 cwd；不依赖真实用户的安装目录、认证、系统 admin skill。

纯应用层注入临时 CodexPaths（Home、CodexHome、AdminSkillRoots、SystemConfigPaths）；phase3 binary 测试复用 Task 10 已实现的 integrationFixture.run 及其 Windows/Unix guard，不能另建无防护运行入口。此 Task 负责扩展矩阵并核对 CI 证据：Linux/macOS 隔离 E2E 实际执行且未跳过，Windows 记录既有 Known Folder active skip，none/help 与注入应用/FS 替代覆盖实际执行。普通目录发现、admin 算法和错误分支用注入 FS 在所有平台测试，不能通过 Windows 映射 /etc 假装验证 Unix admin。已有 helper 保留必要系统变量，GOCOVERDIR 处理不回退。

- [ ] **Step 2: 逐项写断言并确认失败可定位**

| 场景 | 必须断言的可观察结果 |
|---|---|
| active native + blocked + 同名不同路径 | fake argv 中正确 canonical denylist；allow 的所有路径不被禁用 |
| allowed plugin 原配置=false + blocked plugin | true/false 正确，plugin 自身所有 skills 不被普通规则关闭 |
| native + foreign + plugin-disabled + missing | 四状态摘要与控制参数一致，projected 永远 0 |
| all selected / empty set | 全选为全部 true，空 set 为全部 false；只有空全集为 []，bundled/plugins/remote feature 不遗漏 |
| User 中旧 path/name deny | fake 验证生成数组；真实覆盖是否生效由 Task 13 验证 |
| argv/configArgs 混合与跨边界 -c | 配置 args → 用户 args → 控制 args，冲突则 fake 完全未启动 |
| none + 损坏 skillsets/config/cache | 坏 skope config 仍失败；坏 skillsets/Codex 元数据可旁路 |
| dry-run 前后快照 | 不创建新会话或 Codex 文件，允许现有旧会话回收 |
| symlink/Junction 与路径改向 | 前者 canonical 匹配；后者停止并按生命周期清理 |
| active session | Unix PID/退出码沿用 handoff，owner 标 codex，无 settings/addDir，下一次回收 |
| Windows | dry-run/受控 application 执行；原生 handoff 仍明确 unsupported |
| SECRET_SENTINEL 与控制字符 | config/解析错误不泄漏正文；只显示生成控制值，终端字符转义 |
| Claude 回归 | 原 settings/plugin probe/投影完整；Codex 新外来根元数据和拒绝理由正确 |

- [ ] **Step 3: 运行端到端并修复真实失败**

```sh
go test ./internal/cli -run 'TestIntegrationPhaseThree' -count=1 -timeout 180s
go test ./internal/cli -run 'TestIntegrationPhaseTwo' -count=1 -timeout 180s
```

Expected：PASS。仅假 agent 证明 argv/env/cwd/会话行为，不把它描述为 Codex 接受 TOML 或执行隔离的证据。新增 bug 先保留失败测试，再修最小范围，禁止放宽安全断言换取通过。

- [ ] **Step 4: 运行相关包和 Unix race**

```sh
go test ./internal/agent/codex ./internal/skill ./internal/launch ./internal/cli -count=1
```

Linux/macOS/具备 Go 与 cgo 的 WSL 另运行：

```sh
go test -race ./internal/agent/codex ./internal/skill ./internal/launch ./internal/cli -count=1 -timeout 180s
```

Expected：PASS；Windows 不运行依赖缺失 C 工具链的 race。平台 skip 包括 Task 10 公共 binary 来源 guard（Windows active 含 dry-run 在启动前 skip；Unix 固定系统来源存在时 skip，Lstat 其他错误失败），以及确实无法构造的链接或最终 handoff；none/help、注入应用测试和纯算法/解析矩阵继续执行。

- [ ] **Step 5: 提交集成测试及必要修复**

```sh
git add internal/cli/phase3_integration_test.go internal/cli/phase3_application_test.go
git commit -m "test: verify Codex isolation end to end with fake agents"
```

如有业务修复单独显式 stage，保持测试与对应修复可追溯。

### Task 13: 质量门禁、真实 Codex 验收与交付文档

**Files:**

- Modify: `README.md`、`docs/verification.md`、`docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md`。
- Modify only when needed: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`。

- [ ] **Step 1: 类型、依赖与格式审计**

对基线 b329857 核对六个冻结类型、`cmd/skope/main.go`、go.mod/go.sum。允许计划内的辅助类型/消费接口变更；禁止 cli/launch 向上依赖或 adapter 互相 import。只对实际改动文件运行 gofmt/goimports；固定版本 golangci-lint `fmt` 若修改无关文件，检查并只保留本期必要 diff。

```sh
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 fmt
make check
go test -count=1 -coverprofile=coverage.txt ./...
go tool cover -func=coverage.txt
git diff --check
git status --short
```

Windows make 从 Git Bash 运行；PowerShell 用 `go test '-coverprofile=coverage.txt' -count=1 ./...`。Expected：test/build/vet/lint 全绿。记录总覆盖率与 Codex 新包覆盖，目标至少 80%；不通过排除业务文件或填充 getter 测试提升数字。

- [ ] **Step 2: 干净 clone 复验和 CI**

从已提交实现创建一次性 clone，运行 make check 与完整 fake-agent fixture，确认不依赖未跟踪的 fixture/AGENTS.md。按实施会话已有授权推送功能分支并创建/更新 PR；PR 描述行为、spec、验证命令、CLI 输出和 Windows 边界。本仓库功能分支靠 pull_request 触发 CI，只有 push 不能称三平台已验证。

记录同一最终实现提交对应的 Ubuntu/macOS race、Windows test/build 与 lint run URL。CI 失败时保留真实失败并修复，最终实现变更后重新核对产物身份。

- [ ] **Step 3: 绑定真实验收产物**

在执行验收的 Linux/macOS/WSL 环境，从已提交实现的干净 clone 根构建。使用本次新临时目录的绝对路径，不使用 PATH 中的 skope，不沿用 Phase 2 二进制：

```sh
SKOPE_BUILD_COMMIT="$(git rev-parse HEAD)"
SKOPE_BUILD_DIR="$(mktemp -d)"
SKOPE_EXE="$SKOPE_BUILD_DIR/skope"
GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go build -ldflags "-X main.version=phase3-$SKOPE_BUILD_COMMIT" -o "$SKOPE_EXE" ./cmd/skope
"$SKOPE_EXE" version
go version -m "$SKOPE_EXE"
```

Expected：version=phase3-本次提交，VCS revision 与代码一致、vcs.modified=false、OS/架构匹配执行环境。Windows worktree 在 WSL 的 VCS stamp 曾出现指向主工作区的情况，优先独立 Linux clone；任一身份检查失败都先修复构建步骤。

- [ ] **Step 4: 用 skope 复验真实 Codex**

复用 Task 1–2 fixture，在 fixture SKOPE_HOME 写含普通 allowed/blocked、Claude-only、disabled-plugin-only、missing、plugins.codex 和 bundled=false 的手写 skillset，并配置 Step 1 已固定的 Codex 实际 executable。设置好隔离环境后：

```sh
cd "$PHASE3_FIXTURE/repo"
"$SKOPE_EXE" codex -s phase3 --dry-run
"$SKOPE_EXE" codex -s phase3
"$SKOPE_EXE" codex -s none
"$SKOPE_EXE" codex -s phase3 --dry-run
```

确认 features.remote_plugin=false 仍出现在真实最终 argv/有效 feature 中（本地闭环，不宣称认证远端实测）。按 Task 1 已验证的观察协议分别证明普通允许/禁止、同名不同路径、symlink canonical、用户旧 path/name deny 被本次选择接管、plugin 原先关闭但本次允许、plugin 禁止、bundled true/false 与 true 时保留 User 单项 system deny、退出后 owner 保留与下一次回收。真实命令和控制组完整记录，不能只粘贴 dry-run 的正确参数。

实际执行 Codex 可能写自身 fixture 会话/缓存；「不改持久配置」验收比较 config、安装配置与源 skills 字节，区分 agent 自身的常规会话写入。记录模型调用/实际读取事件、版本、fixture、平台和 skope 产物身份。

若必须通过 WSL→Windows bridge，明确它只验证 Windows Codex 能力；桥接必须准确转换每一个 TOML path 和 cwd，并做 TOML 往返核对，不能对整个参数文本做路径替换。Unix 原生 handoff 继续以 CI 为证据。无法证明转换正确时保留未验证，不把实验桥接加入产品。

- [ ] **Step 5: 更新文档与完成条件**

README 增加 `skope codex`、`plugins.codex`、空全集 [] 与全选全部 true 的不同语义、remote_plugin=false 的本地支持限定、无投影与 same-canonical 别名限制、cwd/profile 冲突提示、dry-run 不探测 Codex CLI、实际支持的配置/插件版本范围。更新 Claude 的新增 Codex 项目/admin 外来来源；明确非目标 Claude plugin 枚举仍为 Phase 5。

`docs/verification.md` 追加 Phase 3 的基线/提交、自动门禁、CI、真实验收产物和实验结论。只有 Task 1–13 的实现、测试和真实验收均有证据才标 Phase 3 complete；缺模型额度、认证或真实平台时保持相应步骤未勾选，不能只因代码全绿宣称完成。

- [ ] **Step 6: 提交交付文档并核对最终状态**

```sh
git add README.md docs/verification.md docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md
git commit -m "docs: record Phase 3 Codex implementation and verification"
git diff --check
git status --short
```

若 spec 有实际修订，显式加入。临时 clone/fixture 删除前核对其绝对路径确属本次创建范围；Windows 使用同一 PowerShell 的 `Remove-Item -LiteralPath`，不跨 shell 拼装删除。用户未跟踪 AGENTS.md 留在原位。

## 验收对应表

| 契约 / 风险 | Task | 证据 |
|---|---:|---|
| §7.5 第 1、6、9 条 | 1–3 | 固定 CLI 版本、真实对照、最小 metadata fixtures |
| 原生目录全集、递归深度、admin、bundled 排除 | 2、4、6 | 根矩阵、三平台扫描与真实加载对照 |
| 配置层/安装集合/active root/未知来源 | 2、5–6 | catalog fixtures、类型化错误、无部分清单 |
| ID→全部路径、同名不误开、canonical 别名 | 1、7–8、12–13 | argv golden、symlink/Junction、真实行为 |
| 空全集数组、全选 true 与用户 path/name deny | 1、8、13 | 跨层实验与真实 skope 对照 |
| plugins.codex 与 bundled、多 set 并集 | 2、6、8、11–13 | 开关 argv、摘要计数、真实 plugin/bundled |
| Codex 不投影、foreign 与 missing 区别 | 7、10–12 | 全扫描链路、panic Inspector/Copy traps |
| 参数取值形式、父表、cwd/profile/root markers/marketplaces/remote、内部 -- | 3、9、12 | 两种来源/跨边界、保密 sentinel、none 旁路 |
| TOML 与终端转义分别正确 | 8–9、11–12 | TOML 往返、Unicode/控制字符、生成值白名单 |
| dry-run 无新文件、active owner、失败清理 | 10–13 | 前后快照、fake handoff、Unix 进程证据 |
| 冻结类型、依赖方向、Claude 回归 | 3–13 | 类型 diff、depguard、Phase 2 全链路 |
| 三平台门禁与真实产物身份 | 13 | make check、coverage、CI、version/build info |

## 不在本期实现

- OpenCode adapter、OpenCode 独有来源/配置合并，保留 Phase 4。
- TTY 选择器、skills/doctor/create/edit/delete 和配置写回，保留 Phase 5。
- 非目标 Claude plugin 的完整候选枚举，写入 Phase 5，不为 Codex 启动增加 Claude 安装依赖。
- 账户远端 plugin 枚举与认证允许/禁止闭环、远端安装元数据，归 Phase 5 独立真实门禁；本期 active 强制 remote_plugin=false，none 透传。
- Windows 最终 handoff、`.cmd`/`.bat` 垫片、控制台事件、发布渠道，保留 Phase 6。
- 原生路径的逐入口别名隔离、原子文件系统快照、扫描后新增 skill 的竞态消除；Codex denylist 仍有 spec §13 的已知边界。
- plugin 安装/更新/卸载、依赖闭包、managed policy 绕过、通过替换 CODEX_HOME 迁移用户认证或会话。

## 规划时的文档核对

- 2026-09-06 已按仓库要求执行 Context7 `library 'OpenAI Codex' <详细查询>`，选取 `/openai/codex`，再执行 `docs /openai/codex <同一查询>`。检索提供 path/name rules、bundled 配置及 `.system` 的源码线索，未证明本机版本的数组合并或 plugin 安装 schema，故仍保留真实门禁。
- 官方 skills 文档说明 `.agents/skills` 从 cwd 向上到 repo root、用户/admin 来源、symlink 支持，并给出指向 `SKILL.md` 的禁用示例；不足以证实旧 `.codex/skills` 与缓存布局。[Build skills](https://learn.chatgpt.com/docs/build-skills)。
- 官方高级配置文档说明 `-c` 值按 TOML 解释、CODEX_HOME 存放配置/状态、项目配置按层读取；文档还说明较新 CLI 的 profile 使用独立文件，不能沿用旧 `[profiles.*]` 假设。[Advanced Configuration](https://learn.chatgpt.com/docs/config-file/config-advanced)。
- 官方 plugin 页面说明 plugin 可提供多类能力，但没有给出足以替代 Task 2 的版本固定安装索引契约。[Plugins](https://learn.chatgpt.com/docs/plugins)。
- 上述为最初规划参考；Task 1–2 已记录 CLI 0.153.1 真实本地观察，Task 3 已写回最终契约；后续实现与真实 skope 验收仍待执行。
