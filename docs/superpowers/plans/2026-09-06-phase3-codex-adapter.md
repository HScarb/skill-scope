# skill-scope Phase 3（Codex adapter）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 Phase 2 基线上交付 `skope codex -s <set>`，按 canonical `SKILL.md` 路径生成会话级 denylist，控制 Codex plugin/bundled，正确报告不可用的外来 skill，并通过 fake agent 和真实 Codex 验收。

**Architecture:** 保留六个冻结类型及现有 launch 生命周期。`skill` 补齐目录发现和按显式根进行的有界外来扫描；`agent/codex` 只读安装元数据、生成原生 inventory 和 TOML 控制参数；CLI 显式装配两种 adapter 及目标相关的外来来源。普通 skill 的开关依据选中 ID 对应的全部 Codex 路径，plugin 单独按整个插件控制。

**Tech Stack:** 沿用 Go 1.24.0、Cobra v1.10.2、go-toml/v2 v2.4.3、yaml.v3 v3.0.1、x/sys v0.41.0、标准库 `testing` / `testing/fstest`。不新增生产依赖。

---

**Spec 对应：** `docs/superpowers/specs/2026-09-02-skill-scope-design.md` §3、§4.1–§4.4、§5.3/§5.5、§6.1–§6.3/§6.6、§7.2/§7.4/§7.5 第 1、6、9 条、§8.1/§8.2、§9–§12、§14.3/§14.7。

**执行状态：** 待执行。2026-09-06 仅完成计划编写与代码基线核对；Codex 三项真实门禁在 `docs/verification.md` 中仍为「未验证」。下文的实现方案以 Task 1–3 固定的版本和事实为前提，不能把计划中的预期结果写成实验结论。

**执行技能：** 使用 `@superpowers:executing-plans` 逐任务执行，编码参考 `@karpathy-guidelines`；行为变更使用 `@superpowers:test-driven-development`，失败使用 `@superpowers:systematic-debugging`，最终交付使用 `@superpowers:verification-before-completion`。每个测试矩阵逐行完成红灯、最小实现、绿灯，再处理下一行；每一步控制在约 2–5 分钟，真实实验和全量门禁按实际耗时记录。

新增测试使用标准库 `testing` 和 `xxx_test` 黑盒包，沿用现有 fixture helper；依赖私有生命周期的已有包内测试保持原位置。执行命令中的 `-run` 对应新增测试名前缀，必须确认实际匹配到测试，不能把 `[no tests to run]` 当作通过。

## 基线与实施约束

- 基线为 `main` 的 `b3298576442555fe40305cb9f5b1a4e8bbc2a448`，Phase 2 PR #2 已合并。规划时 `go test -short ./...` 全部通过；这只是已有实现的基线证据。
- 计划位置服从仓库约定 `docs/superpowers/plans/`。当前工作区的未跟踪 `AGENTS.md` 属于用户，不修改、不提交；不要使用 `git add .`。
- 本计划进入版本控制后，实施前使用 `@superpowers:using-git-worktrees` 从包含 Phase 2 的最新 main 创建 `codex/phase3-codex-adapter`。计划编写不建立实现 worktree，也不执行产品改动。
- 仓库没有 `.codegraph/`，直接使用 `rg` 和精确文件读取，不自动建立索引。
- 六个冻结类型为 `Skill`、`Location`、`Adapter`、`Capabilities`、`LaunchPlan`、`Inventory`。本方案不修改它们；若真实实验要求修改，必须先修订 spec §4.2/§7，再调整计划并回归 Claude。
- Linux/macOS 继续使用现有 exec handoff。Windows 本期覆盖扫描、规划、真实 exe 的 dry-run 和注入 handoff 的应用测试；最终 handoff、生产进程身份检查、`.cmd` 解析仍为 Phase 6。
- 真实实验使用临时 HOME、CODEX_HOME、SKOPE_HOME、CLAUDE_CONFIG_DIR 和独立仓库。生产 skope 不重定向 CODEX_HOME，不复制用户认证文件，不修改 Codex 配置或插件安装状态。

| 当前文件 / 函数 | 已有行为 | 本期接法 |
|---|---|---|
| `internal/skill/scope.go:foreignGlobalRoots` | 两条 Codex 全局来源已启用 | 复用根定义，增加项目/admin 根，不复制出另一套发现规则 |
| `internal/skill/scan.go:ScanRoots` | 支持 Root 的可见 agent、PluginID、NamePrefix；skill 根只读直接子项 | 原生目录扫描复用；递归深度必须经真实 Codex 验证后固定 |
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
3. **同一 canonical 路径存在允许和禁止别名时，允许优先并告警。** 这是路径开关无法区分同一目标的限制。选中任一别名后，其余别名共享这个目标；既不生成互相矛盾的条目，也不声称仍可按入口隔离。若别名实际解析规则与此不符，Task 1 必须先修订方案。
4. **每次显式生成 `skills.config`，包括空数组。** 首选接管该数组，用新 denylist 替换用户规则，使用户原先按 path/name 禁用的选中项可用于本次会话；空数组用于清除继承的禁用项。必须实测跨层语义，若 User 层规则单独累积导致无法清除，不得仅增加 `enabled=true` 并假定成功。
5. **允许 plugin 显式写 true，其余写 false。** 全集为实际安装 ID、有效配置中的 plugin 键、允许列表的并集。允许未安装项仍写 true 并告警 missing；安装状态只由有效安装记录判定，配置键或 marketplace 候选不等于已安装。plugin 按整体启用，允许 plugin 的全部 skills 不加入普通 skill denylist。
6. **bundled 独立控制。** 始终生成 `skills.bundled.enabled=<bool>`；`.system` 等真实版本的 bundled 根不进入普通 skill denylist，也不成为 Claude 投影候选。不得因磁盘有 bundled 缓存就在 `bundled=true` 时又逐路径禁用它。

以上第 1、4、5 条需要在 Task 3 澄清 spec §3/§4.3/§7.2/§7.4 的现有简写。尤其 §7.4 的「所有 adapter 都按有效名开关」与 §7.2 的路径 denylist 冲突，不能同时照搬。

### 发现范围与控制面边界

- **根范围跟随已验证的 Codex。** 基础集合是全局 `.agents/skills`、CODEX_HOME/skills，项目 cwd 到 git 根每一级的 `.agents/skills`、`.codex/skills`，Unix `/etc/codex/skills` 和实际安装 plugin 根。无 git 根只扫描 cwd 项目层；`.git` 文件形式的 worktree 也必须识别。
- **原生发现必须完整。** 门禁验证 skill 根内部的递归深度、隐藏目录、链接入口、repo-root cwd 与子目录 cwd 的差异。若当前直接子项 scanner 漏掉 Codex 真正读取的入口，先按证据增加显式扫描策略；不能靠告警继续启动不完整白名单。
- **配置只读投影。** Codex TOML 允许其他业务键，不能用 skope 配置的 `DisallowUnknownFields()` 拒绝整个 Codex 文件；只验证本期读取的插件/skill 规则和安装元数据形状。语法错误或管理字段错误 fail-closed，不回显配置正文、凭据或任意解码值。
- **配置层必须覆盖能引入插件的来源。** Task 2 核对 User、system/admin、项目层、profile 和本地安装元数据。设计按所有适用层的 plugin ID 并集关闭，不能只读用户 config 而漏掉项目新增 ID；无需重建模型、权限、MCP 等无关键的最终配置。
- **活动隔离拒绝变更 cwd 和 profile 的参数。** 本期拒绝 `-C`/`--cd` 与 `-p`/`--profile` 及实际 CLI 支持的附着形式；用户先切换目录再启动。拒绝通过 `-c` 设置受保护的 skills/plugins 顶层或子树，以及影响这些来源的 profile/发现根设置。该限制在 Task 3 写入 spec §6.2；`none` 完整透传。选择拒绝是为了避免实现另一套 Codex cwd/profile 解释器。
- **缺省 profile 也必须处理。** 如果支持版本会从基础配置自动选择 profile，则 Catalog 要么按已验证规则读取其配置层，要么报具名的 unsupported-source 错误；不能只拒绝 CLI profile 参数后忽略基础配置中的选择器。
- **透传中的独立 `--` 不能吞掉控制参数。** 活动隔离拒绝仍留在 agent argv 内的独立 `--`，因为末尾追加的 `-c` 会成为位置参数。skope 自己消费的首个 `--` 不冲突。用户可用 skope 分隔符开始普通透传。
- **外来来源按目标切换。** Claude 目标补齐已验证的 Codex 普通目录和 Codex 安装 plugin 的身份信息；plugin 仍不可投影。Codex 目标扫描已交付的 Claude 普通目录和 legacy commands，用于 unavailable 分类，不调用 Claude plugin list。
- **阶段性来源限制明确保留。** 为 Codex 枚举其他 agent 的完整 plugin inventory 需要额外 agent 探测，不属于本期启动依赖；非目标 Claude plugin 枚举统一留到 Phase 5 的完整候选全集。Task 3 在 §14.5 记录该项，README 明确 missing 指本期已接入来源中无记录。OpenCode 独有来源仍为 Phase 4，不从总 spec 删除。

### 接口与生命周期

```go
// internal/agent/codex/adapter.go：以下四个类型在 Task 6 定义。
type SkillScanner interface {
	ScanCodex(host.Env, []string) (skill.ScanResult, error)
	ScanRoots([]skill.Root) (skill.ScanResult, error)
}

type SourceReader interface {
	Read(context.Context, host.Env) (CatalogSnapshot, error)
}

type Canonicalizer interface {
	EvalSymlinks(string) (string, error)
}

type Options struct {
	Plugins    []string
	Bundled    bool
	AdminRoots []string
}

// internal/agent/codex/catalog.go：Task 5 定义，确保该任务可独立编译。
type CatalogSnapshot struct {
	PluginIDs    []string     // 配置键与安装 ID 并集；不含 marketplace 候选。
	InstalledIDs []string     // 仅实际安装 ID，供 missing 判断。
	SkillRoots   []skill.Root // 有效安装记录指向的 plugin 加载根。
	Warnings     []string
}

func New(scanner SkillScanner, sources SourceReader, paths Canonicalizer, opts Options) Adapter
```

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

**2026-09-06 实测偏差（覆盖本计划后续旧的纯 denylist/数组替换假设）：** Codex CLI 0.153.1 的 User path/name 禁用不被 CLI 空数组清除；显式 canonical path=true 可恢复允许项。产品必须枚举普通路径全集写 true/false，spec §7.2 已修订。Linux skills/list、debug prompt-input、bundled 缓存、参数解析、运行时 profile 对照已完成；Windows Known Folder 忽略 HOME 重定向，原生 Windows/Junction 与真实交互模型调用仍未验。Task 3 须同步后续 Task 6–13 的旧片段，不能照抄仅为禁止项写 false 的方案。证据见 docs/verification.md 第 1、9 条及 testdata/verification/README.md。

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

**Files:**

- Modify: `docs/verification.md`（第 6、9 条和 bundled 补充）。
- Modify: `internal/agent/codex/testdata/verification/README.md`。
- Create: `internal/agent/codex/testdata/catalog/README.md`（当版原始 schema 的脱敏 fixture 说明）。
- Create after verification: `internal/agent/codex/testdata/catalog/` 下按真实 schema 命名的配置/安装/manifest fixtures。

- [ ] **Step 1: 建立不同 cwd 与目录层级的标记 fixture**

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

- [ ] **Step 2: 验证安装索引、缓存和原地来源**

使用不带 MCP、hooks、认证和网络访问的本地测试 plugin，准备 enabled、disabled、同 ID 多版本、仅配置键、仅 marketplace 候选、已卸载但缓存残留、缺失 active root 等对照。所有安装动作仅作用于 fixture CODEX_HOME。

记录实际 ID、安装索引文件名和 schema、有效安装根选择方式、manifest 文件名、skill 根默认值及自定义路径规则、原地开发 plugin 是否存在。明确缓存目录里的哪些条目表示实际安装，哪些只代表候选或历史版本；不预设 `cache/<market>/<plugin>/<version>` 是完整契约，不按版本字符串排序猜当前版本。

- [ ] **Step 3: 验证配置层和插件开关**

分别在 fixture 用户层、项目层、当版支持的 system/admin 层和 profile 中声明不同 plugin ID，核对哪些会进入加载集合；受信任和未信任 workspace 分开记录。只读读取所有潜在项目 plugin 键可以保守扩大关闭集合，但不因此改写 workspace trust。

测试当前 disabled plugin 被 `-c plugins."id".enabled=true` 打开、enabled plugin 被 false 关闭、无配置键的实际安装项、未安装允许项、plugin 内多个 skills、普通 skill 与 plugin skill 同名以及原地 plugin。确认 `skills.config` 是否作用于 plugin skills；产品仍按整体 plugin 控制，不顺带实现插件内部选择。

同时核对能引入新来源的参数与配置：cwd、profile、配置覆盖的父表、发现根、安装路径/marketplace 来源。形成确定的「可读入并集」或「活动隔离拒绝」清单；未支持但会加载 skill/plugin 的来源不得静默漏扫。若 bundled/plugin 开关不存在或不生效，本阶段门禁不通过。

- [ ] **Step 4: 固定测试 fixture 与结论**

将实验确认的元数据结构复制为最小 fixtures，替换绝对路径为测试 token 并在测试加载时绑定 t.TempDir。README 逐个说明含义、支持版本、active 与 stale 的区别；不要写虚构的生产元数据示例。更新第 6、9 条状态，必要时标「不符（已修订 spec §x）」；第 1、6、9 条全通过后才开始 Task 3。

- [ ] **Step 5: 提交证据**

```sh
git add docs/verification.md internal/agent/codex/testdata/verification/README.md internal/agent/codex/testdata/catalog
git commit -m "docs: verify Codex discovery and installed plugin metadata"
```

### Task 3: 将实验结果与设计补充写入总契约

**Files:**

- Modify: `docs/superpowers/specs/2026-09-02-skill-scope-design.md`。
- Modify: `docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md`。

- [ ] **Step 1: 核对三项门禁与本计划前提**

把「安装版本实测」「官方文档」「本项目设计选择」分开。Task 1–2 有未验证项时只完善文档/fixture，不勾选本 Task，不实施 adapter。官方文档或源码只能解释行为，不能替代该二进制的真实门禁。

- [ ] **Step 2: 修订 spec 的具体段落**

| spec 段落 | 必须写清的内容 |
|---|---|
| §3/§4.1 | 实际支持版本、原生根与递归规则、安装来源、bundled 排除根、适用配置层 |
| §4.3/§7.4 | Codex 用 ID 对应路径控制；同有效名不同路径不自动相互允许；同 canonical 别名的限制 |
| §7.2 | 数组接管的实测方案、空数组、plugin true/false、missing 判定；不重定向 CODEX_HOME |
| §6.2 | TOML 父表/引号键、跨参数来源取值、cwd/profile/来源覆盖和内部 `--` 的拒绝边界 |
| §10 | Codex 配置/安装清单不完整、路径 canonicalize 失败、unsupported-source 的 fail-closed 行为 |
| §14.3/§14.5 | 本期跨目标来源范围；非目标 Claude plugin 全集在 Phase 5 接入，保留后续责任 |

根据 Task 2 结果判断是否需要读取额外配置文件或新的扫描策略，并把确切路径/字段填入 Task 4–5。不得让实施者自行选择数组语义或缓存布局。

- [ ] **Step 3: 复核六个冻结类型**

本方案只新增 adapter 内部类型、Root/ScanResult 辅助信息和消费方接口。若确需改变冻结类型，先在 spec 给出完整新定义及兼容方式，再重排本计划，禁止在编码 Task 中顺带修改。

- [ ] **Step 4: 检查文档差异**

```sh
git diff --check
git diff -- docs/superpowers/specs/2026-09-02-skill-scope-design.md docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md
```

Expected：未删除其他 phase 的责任，实验范围与平台限制可追溯；后续任务不再包含尚未决定的配置合并或安装结构分支。

- [ ] **Step 5: 提交契约**

```sh
git add docs/superpowers/specs/2026-09-02-skill-scope-design.md docs/superpowers/plans/2026-09-06-phase3-codex-adapter.md
git commit -m "docs: define verified Phase 3 Codex isolation contract"
```

### Task 4: 补齐 Codex 根发现和通用外来读取

**Files:**

- Create: `internal/skill/codex_scope.go`、`internal/skill/codex_scan_test.go`、`internal/skill/foreign_roots_test.go`。
- Modify: `internal/skill/scope.go`、`internal/skill/scan.go`、`internal/skill/foreign.go`。
- Create: `internal/host/skill_roots_unix.go`、`internal/host/skill_roots_windows.go`、`internal/host/skill_roots_test.go`。
- Test: `internal/skill/scan_links_test.go`、`internal/skill/foreign_unix_test.go`。

- [ ] **Step 1: 写目录与元数据测试**

用 `fstest.MapFS` 包装既有 FileSystem 接口，逐行验证：

| 输入 | 断言 |
|---|---|
| 缺省 / 自定义 CODEX_HOME | 全局根不重复，env.Cwd 和 env.Home 不被修改 |
| cwd 在 repo 子目录、`.git` 为目录/文件 | 仅生成已验证的项目链；Scope 相对仓库根 |
| 无 git 根、父目录有 skills | 只读 cwd 项目根 |
| sibling/descendant、skills 内 group、隐藏目录 | 与 Task 2 的真实读取范围一致 |
| admin 根注入临时目录 | LevelAdmin、SourceCodex、Names[Codex] 正确；测试不读宿主 `/etc` |
| native/foreign 同一个显式 Root | Source/Scope/Kind/PluginID/PluginAgent/Names 一致 |
| foreign command | 原命名空间保留，可报告 command-only，不投影 |
| foreign `.claude-plugin` 目录 | 停止普通 skill 扫描，保留 Phase 2 边界 |
| `.system` 缓存 | 从普通 roots 排除，不成为普通/外来 skill |
| 缺失根 / 已进入目录 EACCES / 断链 | 分别跳过 / 报错 / 报错 |
| foreign 特殊或超限 SKILL.md | 不读无界正文；保留 ID 与 Rejection，Names 不伪造 |

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/skill -run 'TestScanCodex|TestScanForeignRoots' -count=1
```

Expected：因新增方法缺失或新增 root/metadata 断言失败。不要把 fixture 自身路径错误当成需求红灯。

- [ ] **Step 3: 实现最小扫描 API**

```go
// internal/skill/codex_scope.go
func (s Scanner) ScanCodex(env host.Env, adminRoots []string) (ScanResult, error) {
	roots, projectRoot, err := s.CodexRoots(env, adminRoots)
	if err != nil {
		return ScanResult{}, err
	}
	result, err := s.ScanRoots(roots)
	result.ProjectRoot = projectRoot
	return result, err
}
```

增加 `Scanner.CodexRoots(env, adminRoots)`、`Scanner.ClaudeRoots(env)`；后者复用 `claudeScanRoots`，不另写 Claude 遍历。配置层读取需要项目链时提取 `ProjectDirectories(FileSystem, cwd) (root string, dirs []string, err error)`，让现有 Claude 与新 Codex 共用 findGitRoot/projectDirectories 的结果。

`host.CodexAdminSkillRoots()` 在 Linux/macOS 返回 `/etc/codex/skills`，Windows 返回空；若 Task 2 证实 Windows 有真实等价根，先修订契约再实现。CLI 注入该切片，测试显式传临时根；skill 不 import os/runtime。

`ScanForeignGlobals` 委托新的 `ScanForeignRoots`，保留旧调用兼容。抽取读取循环时复制完整 Root 元数据；foreign command 复用命名空间遍历及普通文件检查，增加同样的有界读取。Root 内递归策略只根据 Task 2 需要增加，不能改变已有 Claude 扫描语义。禁止先 ScanRoots 无界读完再给结果加 Rejection。

- [ ] **Step 4: 运行绿灯和现有扫描回归**

```sh
go test ./internal/skill ./internal/host -count=1
go test -short ./...
```

Expected：全部 PASS；Linux/macOS 跑 FIFO 和真实 symlink，Windows 跑已有 Junction 测试。字节限额、read/close 错误传播、源内容不变的 Phase 2 断言继续通过。

- [ ] **Step 5: 提交**

```sh
git add internal/skill internal/host/skill_roots_unix.go internal/host/skill_roots_windows.go internal/host/skill_roots_test.go
git commit -m "feat: discover Codex roots and generalize bounded foreign scans"
```

### Task 5: 只读 Codex 配置与实际安装 catalog

**Files:**

- Create: `internal/agent/codex/catalog.go`、`internal/agent/codex/config.go`、`internal/agent/codex/plugins.go`、`internal/agent/codex/errors.go`。
- Create: `internal/agent/codex/catalog_test.go`、`internal/agent/codex/config_test.go`、`internal/agent/codex/plugins_test.go`。
- Test: `internal/agent/codex/testdata/catalog/` 中 Task 2 已固定的 fixtures。

- [ ] **Step 1: 写 catalog 解析矩阵**

| fixture | 断言 |
|---|---|
| 无 config、无安装目录 | 空集合，成功 |
| 配置 plugin true/false | 两者 ID 均进入 PluginIDs，均不单独证明 installed |
| 实际安装、disabled 安装、同 ID 历史版本 | installed 包含实际 ID；只产生实际 active root |
| marketplace 候选、已卸载残留 | 不冒充 installed，不扫描历史 skill |
| active 安装记录缺根/损坏/未知不可判断形状 | 类型化 inventory 错误；不返回部分成功结果 |
| 已知插件无 skills / 自定义 skills 根 | 分别空 roots / 按已验证 manifest 规则解析 |
| 项目层独有 plugin 键 | 进入关闭全集，路径按实际配置层解析 |
| 受支持的基础 profile / 不支持来源 | 完整读取 / 具名拒绝；不能静默跳过 |
| 无关 model、MCP、认证等合法键 | 不拒绝、不输出、不保存到 Snapshot |
| TOML 非法、plugins 类型错误 | 只报文件路径与静态字段类别，不带正文 sentinel |
| I/O 错误、context 取消 | errors.Is 可识别；不继续读取下一个来源 |
| 调用方修改结果 | 下次 Read 和原 fixture 数据不受影响 |

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/agent/codex -run 'TestCatalog|TestReadCodexConfig|TestParseCodexPlugins' -count=1
```

Expected：新增生产 reader 尚不存在或集合/错误断言失败。

- [ ] **Step 3: 实现 Catalog.Read**

消费方定义最小文件系统接口，由 `host.OSFileSystem` 满足；在 catalog.go 定义前文 CatalogSnapshot，本任务不依赖尚未创建的 adapter.go。读取顺序为解析 CODEX_HOME → 已验证的配置路径集合 → 实际安装元数据 → active plugin manifest/skills roots。使用 Task 2 确认的 schema；未知的无关字段可忽略，无法判断真实安装集合或加载根的形状必须拒绝。

`CatalogSnapshot` 只返回 ID、Root、静态 warnings；PluginIDs/InstalledIDs 去重并排序，roots 按真实 active-record 规则选择，不把排序误用为版本选择。相对 manifest skill 根基于对应 plugin 根解析，路径逃逸或不受支持的额外加载入口按 Task 3 契约拒绝。

语法错误包装为本包 `InventoryError{Path, Field, Cause}`。`Error()` 只格式化 path/静态字段与错误类别；`Unwrap()` 保留原始错误供 errors.Is/As 使用，不能 `%v` 原样输出可能带 TOML 值的 decoder 错误。插件文件读取先检查普通文件类型，防止 FIFO 阻塞；复用 host.OpenRegular，不新增后台进程。

- [ ] **Step 4: 运行绿灯**

```sh
go test ./internal/agent/codex -count=1
go test -short ./...
```

Expected：全部 PASS；未知字段不阻塞，无真实配置读写，无真实 agent/网络调用，输出不含 fixture 中的秘密 sentinel。

- [ ] **Step 5: 提交**

```sh
git add internal/agent/codex
git commit -m "feat: read Codex plugin configuration and installed sources"
```

### Task 6: Codex adapter 与原生 inventory

**Files:**

- Create: `internal/agent/codex/adapter.go`、`internal/agent/codex/inventory.go`、`internal/agent/codex/inventory_test.go`。

- [ ] **Step 1: 写 inventory 行为测试**

使用 fake scanner/catalog 验证：Name=Codex；Capabilities 为 false/true/true；普通根与 plugin 根合并；同 ID 多入口保留；plugin 有 PluginAgentCodex 和 PluginID；SkillNames 只含名称；缺失允许插件只告警一次；配置有键但未安装仍告警；已安装 disabled 项不报 missing；Options 和输出不与输入共享 map/slice。

Catalog/scanner/context 任一失败则返回错误和空 inventory；不执行子进程。构造 New 本身不读取依赖，help/version 因此可安全注册。

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/agent/codex -run 'TestCodexInventory|TestCodexCapabilities|TestCodexOptions' -count=1
```

- [ ] **Step 3: 实现 inventory**

按前文接口构造 Adapter，复制 Plugins/AdminRoots。Inventory 先 ScanCodex，再 Catalog.Read，再扫描已验证 plugin SkillRoots；每个外部步骤之间检查 ctx。使用 `skill.Build` 合并 locations，生成 collisions；PluginIDs 来自 Snapshot，allowed-only ID 留给 Plan 的并集，不伪造 installed 状态。

读取依赖不全时返回静态配置错误；不把 executable 或选择信息塞进 Env。此 Task 不注册到 CLI；Plan 在 Task 8 实现后再添加 `var _ agent.Adapter = Adapter{}`，避免中间提交因缺少 Plan 无法编译。

- [ ] **Step 4: 运行绿灯**

```sh
go test ./internal/agent/codex ./internal/agent/claude ./internal/skill -count=1
```

Expected：全部 PASS；无跨 adapter import，六个冻结类型无 diff。

- [ ] **Step 5: 提交**

```sh
git add internal/agent/codex/adapter.go internal/agent/codex/inventory.go internal/agent/codex/inventory_test.go
git commit -m "feat: build Codex native inventory with plugin visibility"
```

### Task 7: 不支持投影时的原因与 canonical 别名告警

**Files:**

- Modify: `internal/skill/resolve.go`、`internal/skill/resolve_test.go`。
- Modify: `internal/agent/codex/inventory.go`、`internal/agent/codex/inventory_test.go`。
- Test: `internal/launch/projection_test.go`。

- [ ] **Step 1: 写分类和无检查调用测试**

| 选中 ID 的入口 | Codex 结果 |
|---|---|
| 普通 Codex native + 任意 foreign/plugin | native，收集全部合格 native 名称 |
| 只有允许的 Codex plugin | native |
| 只有禁止的 Codex plugin | unavailable / plugin-disabled |
| 只有其他 agent plugin | unavailable / plugin-only |
| 只有 legacy command | unavailable / command-only |
| 有普通 foreign，可同时有 plugin/command | unavailable / projection-unsupported |
| 全集中不存在 | missing |

混合且无普通 foreign 的排除原因顺序固定为目标 plugin-disabled、其他 plugin-only、command-only，给出用户可操作的最直接原因。所有 `Projection=false` 用 panic trap 证明不调用 ProjectionCheck/Inspector/Copier。Claude `Projection=true` 的原选择优先级、拒绝原因、候选回退保持不变。

别名 fixture 包括同 ID 同目标、不同 ID 同目标、同名不同目标；只有「不同 ID 共享同一 Codex canonical 目标」新增静态共享控制告警，不把已有 different-content/effective-name collision 文案当成路径开关结论。

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/skill -run 'TestResolveCodex' -count=1
go test ./internal/agent/codex -run 'TestCodexInventoryCanonicalAliases' -count=1
```

Expected：现有提前返回使 plugin/command 原因断言失败；别名告警缺失。

- [ ] **Step 3: 最小修改原因分支**

先保持 native 优先，再在 `!opts.Projection` 分支检查候选类别并返回固定原因；不进入投影检查。别名告警在 Codex inventory 中由 RealPath 分组、ID 去重后生成，控制身份由 Task 8 再复核。Warnings 说明共享路径开关，不声称改变 skope 的 ID。

- [ ] **Step 4: 运行绿灯与 Claude 回归**

```sh
go test ./internal/skill ./internal/agent/codex ./internal/launch ./internal/agent/claude -count=1
```

Expected：全部 PASS；四状态计数仍按选中 ID，不按 location 数。

- [ ] **Step 5: 提交**

```sh
git add internal/skill/resolve.go internal/skill/resolve_test.go internal/agent/codex/inventory.go internal/agent/codex/inventory_test.go internal/launch/projection_test.go
git commit -m "fix: explain Codex unavailable skills and shared path controls"
```

### Task 8: 生成 TOML 路径 denylist、plugin 与 bundled 参数

**Files:**

- Create: `internal/agent/codex/plan.go`、`internal/agent/codex/paths.go`、`internal/agent/codex/plan_test.go`、`internal/agent/codex/paths_test.go`。
- Create: `internal/agent/codex/testdata/control-args.golden.json`、`internal/agent/codex/testdata/empty-control-args.golden.json`。
- Modify: `.gitattributes`（Codex golden 固定 LF）。

- [ ] **Step 1: 写控制参数矩阵**

选择 `allow`，设置 bundled=false，允许 `keep@market`、`missing@market`，安装/配置另有 `block@market`。只禁止 `/fixture/block/SKILL.md` 时的基准 argv 为：

```json
[
  "-c",
  "skills.config=[{path=\"/fixture/block/SKILL.md\",enabled=false}]",
  "-c",
  "skills.bundled.enabled=false",
  "-c",
  "plugins.\"block@market\".enabled=false",
  "-c",
  "plugins.\"keep@market\".enabled=true",
  "-c",
  "plugins.\"missing@market\".enabled=true"
]
```

该 golden 是 Task 1 数组替换门禁通过后的产品格式，不是已观察输出。测试补充全选/空 inventory 仍输出 `skills.config=[]`；同 ID 多 native 路径全部保留；不同 ID 同有效名仍只允许选中路径；same canonical 允许优先；所有 plugin location 不加入普通 denylist；unavailable/missing 不生成允许路径；输入顺序变化输出稳定。

路径用真实 temp 文件验证绝对与 symlink/Junction 解析，再以 fake canonicalizer 做跨平台 golden。测试引号、反斜杠、中文、空格、制表/换行、非 BMP 字符；每个 `-c` 值用 go-toml 解码回结构后与原值比较，不能只断言字符串含反斜杠。Windows 路径和 Unix 路径分开，不把 Unix 绝对路径交给 Windows filepath 当真实路径。

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/agent/codex -run 'TestCodexPlan|TestCanonical' -count=1
```

- [ ] **Step 3: 实现路径集合与 TOML 编码**

允许集合来自选中 native ID 的普通 Codex location；对 inventory 中每一个普通 Codex location 调用 canonicalizer，包括选中项。将结果转换为本平台绝对路径；空路径、非绝对路径、canonicalize 失败、解析后与 inventory.RealPath 指向不同目标时 fail-closed，避免扫描后链接改向造成误禁用。保留实际大小写，按最终路径字符串稳定排序。

每条 canonical 路径只记录一个 `allowed bool`，通过 OR 合并各发现入口；最终仅输出 false 的路径。已允许 plugin 与普通路径的物理别名也要进入允许集合，避免普通别名 deny 意外关闭整个允许插件；Task 1–2 若证实 plugin 路径不受 skill denylist 影响，在契约中记录并省去该无效分支。

path 值和 plugin 点分键共用只输出 TOML basic quoted string 的小函数。不用 Go `strconv.Quote`，因为它可能输出 TOML 不接受的 `\xNN`、`\a`、`\v`；不用先序列化整份配置再从字符串中截取值。编码函数如下，测试用已固定版本 go-toml 进行语义往返：

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

Plan 的输出顺序固定为 skills.config、bundled、按 ID 排序的 plugins。PluginIDs 与 Options.Plugins 取并集，所有项都写 bool；生成结果不变更 inv/resolved/options。`Files`/`Env` 为空，`sess=nil` 也可以规划；不把原 config.toml 放入 LaunchPlan 或 dry-run。

增加 `var _ agent.Adapter = Adapter{}`。若 Task 1 证实需要不同的数组接管算法，必须已在 Task 3 固定；此处不得临时添加未验证的 true/false 重复规则。

- [ ] **Step 4: 运行绿灯并核对 golden**

```sh
go test ./internal/agent/codex ./internal/agent/claude -count=1
git diff --check
```

Expected：argv golden、TOML 往返、路径改向错误均 PASS；`Plan.Files` 长度为 0，环境无 CODEX_HOME 改写。拒绝更改源目录或创建配置文件来让测试通过。

- [ ] **Step 5: 提交**

```sh
git add internal/agent/codex/plan.go internal/agent/codex/paths.go internal/agent/codex/plan_test.go internal/agent/codex/paths_test.go internal/agent/codex/testdata/control-args.golden.json internal/agent/codex/testdata/empty-control-args.golden.json .gitattributes
git commit -m "feat: generate Codex path and plugin isolation overrides"
```

### Task 9: Codex 冲突参数检测与值保密

**Files:**

- Modify: `internal/cli/conflict.go`、`internal/cli/conflict_test.go`。
- Create: `internal/cli/codex_conflict_test.go`。
- Test: `internal/launch/launch_test.go`、`internal/cli/args_test.go`。

- [ ] **Step 1: 写带来源的参数矩阵**

逐个覆盖 config 与 command-line 来源：`-c value`、`-c=value`、`--config value`、`--config=value`，以及当版支持的 `-cvalue`。保护 skills/plugins 的父表和后代：`skills={...}`、`skills.config=...`、`plugins={...}`、`plugins."p@m".enabled=...`；按实际 CLI 的键解析语义覆盖引号、空白和近似名字。

`skills_extra`、`my.skills`、`model`、`model_reasoning_effort` 等无关键不冲突。引号若在当版 CLI 被当成原始键字符而非 TOML 路径分隔符，记录真实语义；保守拒绝看似受保护的等价写法可以，不能误把实际受保护写法放过。

增加 configArgs 末尾 `-c`、userArgs 首个 `skills.config=...` 的跨边界取值；值中的 `=` 不影响 key 提取；缺失/空 value 返回只含 flag/source 的参数错误。覆盖参数值里出现 `--cd`/`--profile` 文本的正常 prompt，不把单个字符串内部词语当 flag。

按 Task 3 约定测试 cwd/profile/来源覆盖与内部独立 `--` 的拒绝；`none` 在 Service 中跳过 checker。所有错误插入 SECRET_SENTINEL 证明值和原始 parser 错误均不泄漏。

- [ ] **Step 2: 运行红灯**

```sh
go test ./internal/cli -run 'TestCheckConflicts.*Codex|TestCodexConflict' -count=1
```

Expected：当前 Codex 分支直接成功，受保护参数测试失败。

- [ ] **Step 3: 实现 token 提取和受保护键识别**

先拼接两组参数，同时为每个 token 保留来源；不能分别检查两组后丢掉跨边界的 flag/value 关系。返回冲突时 Source 取 flag token 的来源。维护真实 CLI 支持的取值形式；消费配置值后不再把该值当 flag 扫第二遍。

对配置赋值，按真实规则提取 key；若需要 TOML 点分键解析，只解析「key + 固定占位值」，不解析/输出原 RHS。检查顶层 skills/plugins 及 Task 3 的来源选择键；不使用单纯 `strings.HasPrefix(raw, "skills.")`。

`ConflictError` 增加 `Agent skill.Agent`，Error 使用目标名称，Flag 保留 `-c`/`--config` 或冲突 flag，不加入值。现有 Claude 输出保持原样。launch 仍只调用注入的函数；none、首次未知参数停止 skope 解析、原 argv 字节不变等已有行为保持。

- [ ] **Step 4: 运行绿灯**

```sh
go test ./internal/cli -run 'TestCheckConflicts|TestCodexConflict|TestParseLaunchArgs' -count=1
go test ./internal/launch -count=1
```

Expected：两种 agent 冲突矩阵通过；冲突在 factory/inventory/staging/handoff 前阻断；错误无敏感值。

- [ ] **Step 5: 提交**

```sh
git add internal/cli/conflict.go internal/cli/conflict_test.go internal/cli/codex_conflict_test.go internal/launch/launch_test.go internal/cli/args_test.go
git commit -m "feat: reject conflicting Codex configuration and source arguments"
```

### Task 10: 按目标合并外来来源与 launch 编排

**Files:**

- Create: `internal/cli/sources.go`、`internal/cli/sources_test.go`、`internal/launch/codex_test.go`。
- Modify: `internal/launch/launch.go`、`internal/launch/inventory.go`、`internal/launch/inventory_test.go`、`internal/launch/orchestration_test.go`。
- Modify: `internal/skill/scan.go`（ScanResult.Warnings）、`internal/cli/root.go`（Foreign 装配）。
- Update test doubles: `internal/launch/launch_test.go`、`internal/cli/phase2_application_test.go`。

- [ ] **Step 1: 写真实 scanner 到 Service 的测试**

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
go test ./internal/cli -run 'TestForeignSources' -count=1
```

- [ ] **Step 3: 实现消费方接口与 CLI composer**

将 ForeignScanner 替换为前文 `ScanForeign(ctx, env, target, maxBytes)`，Service 原位置传 req.Agent 和已有 `projection.MaxBytes`，更新所有 fake。实现 `cli.foreignSources`：

1. target=Codex：取 Scanner.ClaudeRoots → ScanForeignRoots，保留 command；不调用 claude.Adapter.Inventory 或 proc。
2. target=Claude：取 Scanner.CodexRoots 与 Codex Catalog 的有效 plugin roots → ScanForeignRoots；带完整 PluginAgent/PluginID，bundled 根排除。
3. 其他 target：返回静态 unsupported-target 错误，等待 Phase 4 接入。

提供 `cli.NewForeignScanner(scanner skill.Scanner, catalog codex.SourceReader, adminRoots []string) launch.ForeignScanner` 供生产和黑盒应用测试共同装配；返回私有 composer，复制 adminRoots，构造不读取依赖。Catalog 对外来目标的静态 warnings 放入非冻结 ScanResult.Warnings；`mergeForeignInventory` 复制原生和 foreign warnings，保持原生 PluginIDs/SkillNames 的归属不变。不能将 Codex PluginIDs 混进 Claude 的开关全集。

Codex 自己的普通根在原生扫描中只读一次；外来扫描不再重复 `.agents/skills`/CODEX_HOME。构造两个 adapter 本身无副作用，Codex 启动不要求机器安装 Claude。

- [ ] **Step 4: 运行绿灯与 Phase 2 全链路回归**

```sh
go test ./internal/launch -count=1
go test ./internal/cli -run 'TestForeignSources|TestIntegrationPhaseTwo' -count=1
go test -short ./...
```

Expected：全部 PASS；原生元数据不会被空 Names 的 foreign rejection 覆盖；测试环境新增的 Codex 配置/admin 根全部指向 fixture。

- [ ] **Step 5: 提交**

```sh
git add internal/cli/sources.go internal/cli/sources_test.go internal/cli/root.go internal/cli/phase2_application_test.go internal/launch internal/skill/scan.go
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
- Modify: `internal/cli/integration_test.go`（二进制 fixture 的固定系统来源隔离前置检查）。
- Modify only if needed: 对应失败的业务文件，按失败用例限定修改。

- [ ] **Step 1: 建立独立 fixture 与 fake-agent 启动链**

复用 `testutil.BuildFakeAgent`、`BuildSkope`。完整测试在 `testing.Short()` 时跳过编译 helper；纯应用注入测试保持短测试可执行。每个 fixture 显式设置 HOME/USERPROFILE、CODEX_HOME、CLAUDE_CONFIG_DIR、SKOPE_HOME 和 cwd；不依赖真实用户的安装目录、认证、系统 admin skill。

纯应用层注入临时 adminRoots 和配置来源路径。生产二进制 E2E 的公共 fixture 先对当版所有无法用 HOME 重定向的系统来源入口做 Lstat；若存在，跳过该二进制 E2E 并说明需要隔离环境，绝不进入读取其内容。该前置检查同时覆盖 Claude 回归，因为新 foreign composer 也会接入 Codex admin 来源。CI 必须记录这些 E2E 实际执行且未跳过；普通目录发现、admin 算法和错误分支用注入 FS 在所有平台测试。不能在 Windows 通过映射 `/etc` 假装验证 Unix admin。已有 helper 环境保留必要系统变量，处理 GOCOVERDIR 的现有规则不回退。

- [ ] **Step 2: 逐项写断言并确认失败可定位**

| 场景 | 必须断言的可观察结果 |
|---|---|
| active native + blocked + 同名不同路径 | fake argv 中正确 canonical denylist；allow 的所有路径不被禁用 |
| allowed plugin 原配置=false + blocked plugin | true/false 正确，plugin 自身所有 skills 不被普通规则关闭 |
| native + foreign + plugin-disabled + missing | 四状态摘要与控制参数一致，projected 永远 0 |
| all selected / empty set | 仍含 skills.config=[] 或完整关闭数组，bundled 与 plugins 不遗漏 |
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

Expected：PASS；Windows 不运行依赖缺失 C 工具链的 race。平台 skip 仅针对确实无法构造的链接或最终 handoff，不跳过纯算法/解析矩阵。

- [ ] **Step 5: 提交集成测试及必要修复**

```sh
git add internal/cli/phase3_integration_test.go internal/cli/phase3_application_test.go internal/cli/integration_test.go
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

按 Task 1 已验证的观察协议分别证明普通允许/禁止、同名不同路径、symlink canonical、用户旧 path/name deny 被本次选择接管、plugin 原先关闭但本次允许、plugin 禁止、bundled true/false、退出后 owner 保留与下一次回收。真实命令和控制组完整记录，不能只粘贴 dry-run 的正确参数。

实际执行 Codex 可能写自身 fixture 会话/缓存；「不改持久配置」验收比较 config、安装配置与源 skills 字节，区分 agent 自身的常规会话写入。记录模型调用/实际读取事件、版本、fixture、平台和 skope 产物身份。

若必须通过 WSL→Windows bridge，明确它只验证 Windows Codex 能力；桥接必须准确转换每一个 TOML path 和 cwd，并做 TOML 往返核对，不能对整个参数文本做路径替换。Unix 原生 handoff 继续以 CI 为证据。无法证明转换正确时保留未验证，不把实验桥接加入产品。

- [ ] **Step 5: 更新文档与完成条件**

README 增加 `skope codex`、`plugins.codex`、empty denylist 的选择语义、无投影与 same-canonical 别名限制、cwd/profile 冲突提示、dry-run 不探测 Codex CLI、实际支持的配置/插件版本范围。更新 Claude 的新增 Codex 项目/admin 外来来源；明确非目标 Claude plugin 枚举仍为 Phase 5。

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
| 空数组与用户 path/name deny | 1、8、13 | 跨层实验与真实 skope 对照 |
| plugins.codex 与 bundled、多 set 并集 | 2、6、8、11–13 | 开关 argv、摘要计数、真实 plugin/bundled |
| Codex 不投影、foreign 与 missing 区别 | 7、10–12 | 全扫描链路、panic Inspector/Copy traps |
| 参数四种取值形式、父表、cwd/profile、内部 -- | 3、9、12 | 两种来源/跨边界、保密 sentinel、none 旁路 |
| TOML 与终端转义分别正确 | 8–9、11–12 | TOML 往返、Unicode/控制字符、生成值白名单 |
| dry-run 无新文件、active owner、失败清理 | 10–13 | 前后快照、fake handoff、Unix 进程证据 |
| 冻结类型、依赖方向、Claude 回归 | 3–13 | 类型 diff、depguard、Phase 2 全链路 |
| 三平台门禁与真实产物身份 | 13 | make check、coverage、CI、version/build info |

## 不在本期实现

- OpenCode adapter、OpenCode 独有来源/配置合并，保留 Phase 4。
- TTY 选择器、skills/doctor/create/edit/delete 和配置写回，保留 Phase 5。
- 非目标 Claude plugin 的完整候选枚举，写入 Phase 5，不为 Codex 启动增加 Claude 安装依赖。
- Windows 最终 handoff、`.cmd`/`.bat` 垫片、控制台事件、发布渠道，保留 Phase 6。
- 原生路径的逐入口别名隔离、原子文件系统快照、扫描后新增 skill 的竞态消除；Codex denylist 仍有 spec §13 的已知边界。
- plugin 安装/更新/卸载、依赖闭包、managed policy 绕过、通过替换 CODEX_HOME 迁移用户认证或会话。

## 规划时的文档核对

- 2026-09-06 已按仓库要求执行 Context7 `library 'OpenAI Codex' <详细查询>`，选取 `/openai/codex`，再执行 `docs /openai/codex <同一查询>`。检索提供 path/name rules、bundled 配置及 `.system` 的源码线索，未证明本机版本的数组合并或 plugin 安装 schema，故仍保留真实门禁。
- 官方 skills 文档说明 `.agents/skills` 从 cwd 向上到 repo root、用户/admin 来源、symlink 支持，并给出指向 `SKILL.md` 的禁用示例；不足以证实旧 `.codex/skills` 与缓存布局。[Build skills](https://learn.chatgpt.com/docs/build-skills)。
- 官方高级配置文档说明 `-c` 值按 TOML 解释、CODEX_HOME 存放配置/状态、项目配置按层读取；文档还说明较新 CLI 的 profile 使用独立文件，不能沿用旧 `[profiles.*]` 假设。[Advanced Configuration](https://learn.chatgpt.com/docs/config-file/config-advanced)。
- 官方 plugin 页面说明 plugin 可提供多类能力，但没有给出足以替代 Task 2 的版本固定安装索引契约。[Plugins](https://learn.chatgpt.com/docs/plugins)。
- 上述为规划参考；Task 1–2 应记录实际 CLI 版本和观察，Task 3 再将最终事实写回实现契约。
