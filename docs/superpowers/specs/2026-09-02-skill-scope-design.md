# skill-scope（`skope`）设计文档

日期：2026-09-02
状态：Draft。实施按 §14 分期推进；§7.5 的真实 agent 验证按 agent 分组，每组通过前不进入对应 adapter 的实现。核心类型在 Phase 1 结束时冻结
参考实现：`cc-switch-scope`（Node.js，`ccscope` 命令）的 skillset 隔离能力

## 1. 定位

`skope` 是一个会话级 coding agent 启动器。它在启动 `claude`、`codex`、`opencode` 之前，按用户选择的 skill set 对本次会话可见的 skill、plugin 和内置 skill 做白名单隔离，然后把控制权交给 agent 进程。

目标：

- 一份 skill set 定义在三个 agent 之间共用；skill 全集是三个 agent 各自能发现的目录的并集（全局级 + 项目级）。
- 每个终端会话独立选择 skill set，不改动任何 agent 的持久化配置。
- 对 agent 自己能发现的 skill 用原生开关做白名单；对它看不到的「外来」skill，在支持的 agent 上复制到会话临时目录并注入。
- 启动开销可忽略；Unix 上以 exec() 替换进程，agent 直接接管终端。

非目标：

- 不管理 skill 的安装、更新和删除，skope 不拥有任何 skill 文件。
- 不集成 CC-Switch 供应商切换。
- 不支持用户自定义 agent，v1 只内置三个。
- 不是操作系统安全沙箱。

## 2. 技术栈

| 用途 | 选择 | 理由 |
|---|---|---|
| 语言 | Go 1.24+ | 单文件静态二进制，启动约 5 ms，Unix 有 `syscall.Exec`，不依赖用户机器的 Node 版本 |
| 命令分发 | spf13/cobra | 子命令、help、shell 补全。agent 启动命令设 `DisableFlagParsing`，自行识别 skope 选项后其余透传 |
| 交互 UI | charmbracelet/huh + lipgloss | 多选、单选、确认、输入开箱即用，Windows 终端支持良好。已知风险：传统 conhost 上渲染可能有兼容问题，`doctor` 检测终端类型并在 conhost 下提示建议使用 Windows Terminal |
| 配置 | pelletier/go-toml/v2 | TOML 人手可写、支持注释 |
| Windows 进程 | golang.org/x/sys/windows | Job Object、控制台 Ctrl 事件、解析 npm `.cmd` 垫片 |
| 测试 | 标准库 `testing`、`testing/fstest`、google/go-cmp | 扫描逻辑基于 `io/fs`，用内存 fs 测试 |
| 发布 | goreleaser | GitHub Release、Homebrew tap、Scoop bucket 一份配置产出 |
| 质量 | gofmt、go vet、golangci-lint、`go test -race` | 标准组合 |

被否决的候选及原因：Node.js/Bun 没有 exec()、依赖用户 Node 版本、Windows 上有 `.cmd` 垫片层；Rust 在本项目关键能力上与 Go 等价但迭代慢。

## 3. 三个 agent 的管控事实（2026-09 核实）

| | Claude Code | Codex CLI | OpenCode |
|---|---|---|---|
| 发现目录 | `$CLAUDE_CONFIG_DIR/skills`（缺省 `~/.claude/skills`），项目 `.claude/skills`（含嵌套子目录，名字带作用域如 `apps/web:verify`）、`.claude/commands`、plugin `skills/`、`--add-dir` 目录下的 `.claude/skills`。不扫 `.agents/skills` | `~/.agents/skills`、项目 `.agents/skills`（cwd 向上到仓库根每一级）、项目 `.codex/skills`、`$CODEX_HOME/skills`（已废弃但仍读）、`/etc/codex/skills`、plugin | `.opencode/skill(s)`、`~/.config/opencode/skill(s)`、`~/.opencode`、`OPENCODE_CONFIG_DIR`、兼容 `.claude/skills` 与 `.agents/skills`（全局与项目级）、配置 `skills.paths`（本地目录）、`skills.urls`（远程） |
| 按名开关 | settings `skillOverrides`（`on`/`off`/`name-only`/`user-invocable-only`，不作用于 plugin skill）、`enabledPlugins`（键为 `plugin@marketplace`）、`disableBundledSkills` | `[[skills.config]]`：`path`（`AbsolutePathBuf`，规则构建时 canonicalize，精确集合匹配）或 `name`（与已加载 skill 名精确比较后折算为路径）+ `enabled`；只从 User 层与 `-c` 覆盖层读取；denylist 语义，无「默认全关」；`skills.bundled.enabled`；`[plugins."id"] enabled` | `permission.skill` 通配 allow/deny/ask，后写规则胜出，原生支持白名单；plugin 无 per-plugin 开关 |
| 注入额外 skill 目录 | `--add-dir <dir>`（同时授予文件访问权）或 `--plugin-dir` | 无干净入口；`CODEX_HOME` 会连带重定向 config/auth/sessions | `skills.paths` 或 `OPENCODE_CONFIG_DIR`（追加而非替换） |
| 会话级注入通道 | `--settings <file>` | `-c key=value`，值按 TOML 解析 | 环境变量 `OPENCODE_CONFIG_CONTENT`（内联 JSON，最后合并）；无 `--config` 参数 |
| 有效 skill 名 | 目录名，嵌套项目目录带作用域前缀 | frontmatter `name` | frontmatter `name` |

三个 agent 的开关都以 skill 名为键。因此对 agent 已经能看到的 skill，不能「关掉原生 + 注入副本」，只能原生打开；注入只针对该 agent 看不到的 skill。

## 4. 数据模型

### 4.1 扫描范围

| 层级 | 目录 | 可见 agent |
|---|---|---|
| 全局 | `$CLAUDE_CONFIG_DIR/skills/*/SKILL.md`（缺省 `~/.claude/skills`） | Claude、OpenCode |
| 全局 | `$CLAUDE_CONFIG_DIR/commands/**/*.md`（缺省 `~/.claude/commands`，legacy command） | Claude |
| 全局 | `~/.agents/skills/*/SKILL.md` | Codex、OpenCode |
| 全局 | `$CODEX_HOME/skills/*/SKILL.md`（默认 `~/.codex`） | Codex |
| 全局 | `~/.config/opencode/skill(s)/**/SKILL.md`、`~/.opencode/skill(s)/**/SKILL.md`、`$OPENCODE_CONFIG_DIR/skill(s)/**/SKILL.md` | OpenCode |
| 全局 | OpenCode 配置（§7.3）中 `skills.paths` 列出的目录 | OpenCode |
| admin | `/etc/codex/skills/*/SKILL.md`（只读扫描） | Codex |
| 项目 | cwd 向上到 git 根的每一级 `.claude/skills`（含嵌套子目录）、`.claude/commands/**` | Claude、OpenCode |
| 项目 | 同上范围的 `.agents/skills` | Codex、OpenCode |
| 项目 | 同上范围的 `.codex/skills` | Codex |
| 项目 | 同上范围的 `.opencode/skill(s)` | OpenCode |
| plugin | Claude `plugin list --json` 的真实安装项所对应加载根下 `skills/*/SKILL.md`，含 `@skills-dir` 自动 plugin；加载根解析见下。Codex `$CODEX_HOME/plugins` 缓存中的 skill | 各自 agent |

不扫描的来源：OpenCode `skills.urls`（远程），检测到时告警「该来源不受 skope 管理」。

遍历规则：

- 找不到 git 根时只扫 cwd 自身的项目级目录。
- 递归查找 `.claude`、`.agents` 等目录时跳过 `.git`、`node_modules` 和任意符号链接目录。
- 允许 `skills/<name>` 条目本身是 symlink 或 Junction，只跟随它检查直接子项 `SKILL.md`，不递归进入链接目标的其他目录。
- 目录不存在（ENOENT）是正常情况。已进入的目录出现 EACCES、断链等错误时抛出清单错误。

Claude plugin 枚举依据为 2.1.259 的真实实验（`docs/verification.md` 第 10 条）：

- `plugin list --json` 返回数组；真实项有 `id`、`enabled`、`scope`、绝对 `installPath`，同 ID 多次安装保留多行及原顺序。普通 project/local 项带 `projectPath`；自动 project plugin 不带该字段。`--available` 会改为对象且混入市场候选，不能用于本契约。
- installed 集合包含所有真实行的 ID，与当前 enabled 状态无关。列表也列出其他项目的安装；不得先按 cwd 过滤 installed 集合再判 missing。缓存加载根按列表原顺序取首个适用记录：user 对所有 cwd 适用，project/local 需 cwd 等于或位于 `projectPath` 目录内（按路径组件比较，不能字符串前缀）。不排序 scope 或版本号，不把历史缓存目录当当前入口。没有适用记录时只保留 installed ID，不建 native location；元数据损坏、未知来源或应存在根读失败才 fail-closed。后续 Claude 根据允许项自动安装的新位置不在本次 inventory 的预知范围。
- 对 `@skills-dir`，直接使用 list 的原地 `installPath`。自动 plugin 来源包括 `$CLAUDE_CONFIG_DIR/skills/<name>/.claude-plugin/plugin.json` 与 cwd `.claude/skills/<name>/.claude-plugin/plugin.json`；项目项要求 workspace 已信任，且不向父目录扫描。普通 skill 扫描遇到 plugin manifest 目录必须停止，避免把内部 skill 重复当普通 native。
- 精确占位形状 `id="(suppressed)@skills-dir"`、`scope="project"`、`enabled=false`、`installPath=""`、`version="unknown"`、字符串数组 `notes` 表示未信任项目的自动 plugin 被跳过；转为静态告警，不进入 installed/PluginIDs，不原样输出 notes。其他空路径或损坏关键字段 fail-closed。
- 普通 marketplace plugin 必须读取 `$CLAUDE_CONFIG_DIR/plugins/known_marketplaces.json` 对应 marketplace 的 `source.source` 和 `installLocation`。`source.source="directory"` 时读取 `<installLocation>/.claude-plugin/marketplace.json`，按精确 plugin name 找到相对字符串 `source`，加载根为 marketplace 根加该路径；真实 Claude 会原地加载，list 的 cache 路径可能已有旧内容。其他已知缓存来源（git/github/url）使用适用 list 项的 `installPath`。无法解析的来源/非字符串 directory plugin source 明确 fail-closed；不再运行模型探测，也不读取完整用户配置作为替代。
- plugin skill 的逻辑 ID 仍是 basename；Claude 有效名为 `<plugin manifest name>:<basename>`，不能用 marketplace ID 或 frontmatter name 代替 namespace。manifest name 缺失/错误应报清单错误。

外来 `SKILL.md` 在 frontmatter 解析前检查入口和打开句柄的文件类型，只打开普通文件，按 §8.3 的 20 MiB 限额有界读取（最多限额加 1 byte）。特殊文件及超限入口保留构建 ID 所需的 location 和结构化拒绝原因；Names 留空，不伪造 frontmatter 回退值，不记 missing。普通文件打开由注入接口完成，必须防止 FIFO 在打开阶段阻塞；真实 I/O 或 frontmatter 错误仍 fail-closed。

### 4.2 结构

```go
type Agent  string // "claude" | "codex" | "opencode"
type Kind   string // "skill" | "command"
type Level  string // "global" | "project" | "plugin" | "admin"
type Source string // "claude" | "agents" | "codex" | "opencode" | "opencode-paths"

// 一条逻辑 skill，skill set 里存的是 ID
type Skill struct {
    ID        string
    Locations []Location // 全部发现入口，按优先级排序
}

// 一个发现入口。去重键是 (Source, DiscoveryPath)
type Location struct {
    Kind            Kind
    DiscoveryPath   string           // agent 实际会读到的路径，未解析链接
    RealPath        string           // 解析链接后的路径，只用于标注「同内容」
    Level           Level
    Source          Source
    Scope           string           // 嵌套项目目录相对仓库根的路径，如 apps/web
    FrontmatterName string           // SKILL.md 的 name 字段，空表示缺失
    Names           map[Agent]string // 该入口在每个可见 agent 中的有效名
    PluginID        string           // 来自 plugin 时填写
    PluginAgent     Agent
}
```

### 4.3 身份规则

- **ID**：`SKILL.md` 所在目录的 basename；嵌套项目目录下为 `<scope>:<basename>`（如 `apps/web:verify`），与 Claude 的作用域命名一致。legacy command 的 ID 是文件名去掉 `.md`，子目录用 `:` 表达命名空间。
- **同 ID 合并**：同一 ID 的多个发现入口视为同一逻辑 skill 的多个 location。command 与 skill 同 ID 也合并，`Kind` 由各 location 携带。
- **不按 realpath 去重**：Junction 把同一目录同时挂到 `.claude/skills/foo` 和 `.agents/skills/foo` 时，两个入口都保留，否则启动 Codex 时会误判 unavailable。`RealPath` 相同的入口在 `skope skills` 中标注「同一目标」。
- **有效名**：`Names[claude]` = 目录名加作用域前缀；`Names[codex]`、`Names[opencode]` = `FrontmatterName`，缺失时回退目录名。只为该入口可见的 agent 填写。
- **frontmatter 解析**：`SKILL.md` 第一行不是 `---` 时视为没有 frontmatter；合法 frontmatter 没有 `name` 时 `FrontmatterName` 为空。第一行是 `---` 但 YAML 非法、缺少结束分隔符或 `name` 不是字符串时 fail-closed，错误必须包含该 `SKILL.md` 的 `DiscoveryPath`。选择 fail-closed，因为 Codex/OpenCode 的有效名依赖该字段，静默回退会让白名单命中错误对象。
- **选择语义**：skill set 选中一个 ID，即在每个 agent 上开启该 ID 全部符合 §4.4 条件的可见 location 有效名；plugin 仍需独立允许。不提供按路径限定选择的语法。
- **碰撞告警**（`skope skills` 与启动摘要均报告）：
  - 同 ID 多入口且 `RealPath` 不同：内容可能不一致。
  - `FrontmatterName` 与目录名不一致。
  - 不同 ID 在同一 agent 上有效名相同（如根级 `verify` 与 `apps/web:verify` 在 OpenCode 中都叫 `verify`）：允许其一即允许两者，agent 自身按其规则取其一。
- **Location 优先级**：层级按 `project > global > admin > plugin`；同级按 `claude > agents > codex > opencode > opencode-paths`。admin `/etc/codex` 低于用户全局配置，但仍是 agent 原生入口，优先于不可跨 agent 投影的 plugin。若 Phase 3 的真实 Codex 验证推翻该顺序，必须先修订本文档再调整实现。

### 4.4 可见性与投影判定

启动 agent X 时，对白名单里的每个 ID：

1. 若任一普通 location 对 X 可见，走 X 的原生开关，记为 `native`；plugin location 只有其 PluginID 被对应 `plugins` 允许列表选中时才可作为 native。选中 skill ID 不隐式开启 plugin，普通同 ID native 仍优先。
2. 否则若 X 支持投影（Claude、OpenCode），从 location 中按优先级选一个通过 §8.3 自包含检查后复制到会话目录注入，记为 `projected`。v1 不投影 plugin 级 location。
3. 否则记为 `unavailable`，摘要里给出原因：Codex 不支持投影、仅存在于未允许的 plugin、入口特殊/超限、目标冲突、或自包含检查失败。拒绝一个候选后继续尝试下一个 location。
4. ID 在全集中不存在时记为 `missing`，告警继续。

command 类 location 仅 Claude 可见，不可投影，但必须进入 Claude 的白名单全集。

skill set 只存 ID，不存路径。来源每次启动重新扫描解析。

投影按 selection 顺序保留首个通过检查且目标未被占用的候选；后续目标冲突 ID 为 unavailable。所有平台对目标路径逐组件采用 `strings.EqualFold` 保守判断大小写别名（如 Foo/foo）；即使 Linux 目录允许并存也拒绝，以便 dry-run 无需写入探测。ID 与有效名保留原大小写。这不是所有文件系统 Unicode 等价规则的完整模型，写入还须独占创建兜底。

## 5. 配置文件

### 5.1 目录与文件

默认目录 `~/.skope/`，`SKOPE_HOME` 环境变量可覆盖为目录路径：trim 后展开 `~` 前缀，必须是绝对路径，否则报错。目录下：

| 文件 | 内容 | 谁读 |
|---|---|---|
| `config.toml` | agent 启动配置 | 每次启动都读；缺失视为空；损坏一律报错，包括 `-s none` |
| `skillsets.toml` | skill set 定义 | 只在需要解析 skill set 时读；`-s none` 不读 |
| `sessions/` | 会话目录 | §8 |

拆成两个文件的原因：`-s none` 承诺绕过 skill set 配置损坏，但 agent 的 `command`/`args` 仍必须生效，不能从损坏 TOML 中「部分恢复」。

读取不创建目录；`create`/`edit` 写入时才创建父目录，目录权限 `0o700`。

### 5.2 `config.toml`

```toml
version = 1

[agents.claude]
command = "claude"                          # 可选，缺省按 PATH 查找
args = ["--dangerously-skip-permissions"]   # 可选，每次启动追加

[agents.codex]
[agents.opencode]
```

校验：`version` 必填且为 `1`；`agents.<name>` 只接受三个内置名；`command` 字符串；`args` 字符串数组；未知字段报 TOML 路径。`args` 中出现 §6.2 的隔离冲突参数时不在加载阶段报错，在活动 skill set 启动时与透传参数一并检测。

### 5.3 `skillsets.toml`

```toml
version = 1

[skillsets.dev]
description = "日常开发"
skills = ["commit", "code-review", "apps/web:verify"]
bundled = true                              # 缺省 true

[skillsets.dev.plugins]                     # 按 agent 分组，只对该 agent 生效
claude = ["commit-commands@claude-plugins-official"]
codex  = ["demo@market"]

[skillsets.minimal]
skills = []
bundled = false
```

校验：

- `version` 必填，只接受 `1`。
- skill set 名 trim 后非空，只允许字母、数字、点、下划线、短横线，且不可以短横线开头（否则 `-s <name>` 无法与选项区分）；`none` 保留；不可含逗号。
- `skills` 字符串数组（可空），元素 trim 后非空且不重复；允许 `:` 与 `/`。
- `plugins` 只接受 `claude`、`codex` 两个键；元素必须是 `name@marketplace` 两段。出现 `opencode` 键报错，说明 v1 不管理 OpenCode plugin。
- `description` 若存在必须是字符串。
- `bundled` 若存在必须是布尔，缺省归一化为 `true`。
- 未知字段、类型错误均报出 TOML 路径。

### 5.4 写入

`create`/`edit`/`delete` 写 `skillsets.toml`：

1. 读取当前文件内容并计算哈希（不存在记为空）。
2. 在内存中修改，序列化。
3. 写同目录随机临时文件，`0o600`。
4. 替换前重新读取目标文件哈希，与第 1 步不一致则中止并提示「文件已被其他进程修改，请重试」，删除临时文件。
5. `os.Rename` 原子替换（Windows 下 Go 使用 `MoveFileEx` 的 `REPLACE_EXISTING`）。

失败时保留原文件并清理临时文件。

`create`/`edit` 是整文件重写：不保留用户手写的注释，skill set 顺序按内存中的有序结构（文件出现顺序，新项追加在末尾）序列化，§6.1 选择器的「按配置顺序」以此为准。go-toml/v2 无注释 round-trip 能力，接受此取舍。

### 5.5 多 skill set 并集

`-s a,b`：`skills` 求并集；`plugins` 按 agent 分别求并集；`bundled` 任一为 true 则为 true；`description` 不参与合并；显示名保留顺序，如 `a+b`。

## 6. CLI

```
skope claude   [-s <set>[,<set>]] [--dry-run] [agent 参数...]
skope codex    [-s <set>[,<set>]] [--dry-run] [agent 参数...]
skope opencode [-s <set>[,<set>]] [--dry-run] [agent 参数...]

skope list                  # 列出 skill set
skope create [name]         # 交互向导
skope edit <name>
skope delete <name> [-y]
skope skills [--agent <x>]  # 打印扫描全集
skope doctor                # 环境检查
skope completion <shell>    # cobra 内置
skope version
```

### 6.1 启动命令的参数规则

- skope 只识别 `-s`/`--set` 和 `--dry-run`。遇到第一个不认识的参数或显式 `--` 后，其余全部原样透传给 agent。
- `-s none` 显式不隔离；`none` 不可与其他名字组合。
- 同一命令重复 `-s` 报错。
- 未传 `-s` 且 stdin/stdout 为 TTY 时弹单选器：首项固定为「不隔离，保持 agent 默认」，其后按配置顺序列出各 skill set 及描述。用户取消则退出码 0，不启动。
- 未传 `-s` 且非 TTY 时报错，提示显式传参。IDE 任务、CI 等非交互场景因此必须显式声明 skill set 或 `none`，这是有意取舍：自动静默选择会掩盖隔离状态。
- 最终 argv 顺序：`config.toml` 的 `args`，然后用户透传参数，最后 skope 的隔离控制参数。控制参数放最后不是为了依赖「后者获胜」，而是配合 §6.2 的检测保证控制面参数在 argv 中只出现一次。

### 6.2 与隔离冲突的参数

活动 skill set 下，对「`config.toml` 的 `args` + 用户透传参数」的并集做检测，命中即拒绝启动，报错只打印参数名和来源（配置或命令行），不打印参数值：

- Claude：`--settings`、`--setting-sources`、`--plugin-dir`、`--plugin-url`、`--add-dir`（含 `--flag=value` 形式）
- Codex：`-c`/`--config` 的键以 `skills.` 或 `plugins.` 开头
- OpenCode：无 CLI 冲突参数

`-s none` 下不做此检查。

### 6.3 `--dry-run`

执行完整的扫描与规划（含 `claude plugin list`），然后打印并退出，不创建会话目录、不启动 agent。`-s none` 时无隔离规划，跳过扫描与 `plugin list`，仅打印 argv 与环境变量。输出字段白名单：

- 最终 argv（经 §9.1 转义）
- skope 新增或修改的环境变量名；值按 §9.1 脱敏规则显示
- 会话目录将包含的文件相对路径清单
- skope 生成的 settings / 配置 JSON 内容。OpenCode 只打印 skope 注入的键，不打印与用户已有 `OPENCODE_CONFIG_CONTENT` 合并后的结果
- 摘要与告警

### 6.4 管理命令契约

| 命令 | 行为 | 非 TTY | 退出码 |
|---|---|---|---|
| `list` | 表格：名字、描述、skills 数、各 agent plugins 数、bundled。文件不存在时打印路径与「暂无配置」 | 可用 | 0；文件损坏 1 |
| `create [name]` | 向导见 §6.5；名字已存在则转 edit | 报错 | 0；取消 0；写入失败 1 |
| `edit <name>` | 只允许已存在项 | 报错 | 同上；不存在 1 |
| `delete <name>` | 确认后删除；`-y` 跳过确认 | 无 `-y` 报错 | 0；不存在 1 |
| `skills [--agent x]` | 每个 ID 一行：ID、各 location 的 Kind/Level/Source/DiscoveryPath、对三个 agent 的可见性与有效名、§4.3 碰撞告警。`--agent` 只显示对该 agent 可见的入口 | 可用 | 0；扫描 I/O 错误 1 |
| `doctor` | 对每个 agent：可执行文件解析结果（含 Windows 垫片真实目标）、`<agent> --version` 输出（§9.2 子进程契约，超时记为 unknown 不中止）、`$CLAUDE_CONFIG_DIR`/`$CODEX_HOME`/`$OPENCODE_CONFIG_DIR`/`$OPENCODE_CONFIG` 的解析结果；两个配置文件的存在与校验状态；`SKOPE_HOME` 解析结果；sessions 目录中残留会话数 | 可用 | 全部正常 0；agent 可执行文件缺失或解析失败、配置损坏为 1；仅未安装某 agent（用户未配 `command` 且 PATH 无）记 0 并标注 not installed，不与其他错误混计 |

### 6.5 create/edit 向导

1. 名字（create 未传时输入并校验）
2. 描述
3. 多选 skill，候选为扫描全集，每项显示 ID、来源摘要、可见 agent、碰撞标记
4. 多选 Claude plugin（候选来自 `claude plugin list --json`）、多选 Codex plugin（候选来自 `$CODEX_HOME/config.toml` 与 plugin 缓存）。对应 agent 未安装则跳过该步并提示
5. bundled 开关
6. 摘要确认后按 §5.4 写入

### 6.6 摘要输出

```
→ Skillset [dev] for claude
  skills:  4 native, 2 projected, 1 unavailable, 0 missing
           unavailable: foo (only in ~/.codex/skills, Codex-only source)
  plugins: 1 allowed, 3 disabled (Claude may enable dependencies of allowed plugins)
  bundled: on
→ Launching claude
```

计数来自实际 inventory 与 plan。plugin 一行只报告 skope 写入的 true/false 数量，不声称已解析依赖闭包或 managed policy。

## 7. Agent 适配层

```go
type Adapter interface {
    Name() Agent
    Capabilities() Capabilities
    // 该 agent 自己能发现的 skill/plugin 全集，及从其 settings 中读到的已有开关键
    Inventory(ctx context.Context, env Env) (Inventory, error)
    // 根据白名单解析结果与会话目录生成启动方案
    Plan(resolved Resolved, inv Inventory, session *Session) (LaunchPlan, error)
}

type Capabilities struct {
    Projection     bool // 支持把外来 skill 复制到会话目录注入
    TogglePlugins  bool // 支持按 plugin 粒度开关（进入全集逐个写开/关）
    ToggleBundled  bool // 支持关闭 agent 内置 skill（含 OpenCode 以 deny 内置 skill 名实现的方式）
}

type LaunchPlan struct {
    ControlArgs []string          // 追加在 argv 末尾
    Env         map[string]string // 覆盖或新增
    Files       []PlannedFile     // 写入会话目录
}
```

Capabilities 取值：

| | Projection | TogglePlugins | ToggleBundled |
|---|---|---|---|
| Claude | true | true | true |
| Codex | false | true | true |
| OpenCode | true | false（plugin 不管理） | true（deny `customize-opencode`，仅覆盖已知内置清单） |

核心流程只依赖接口。三个 agent 的差异全部封装在 `internal/agent/<name>/` 包内。

### 7.1 Claude

| 步骤 | 内容 |
|---|---|
| Inventory | 扫描 Claude 可见目录；读 user/project/local 三层 settings 的 `skillOverrides` 与 `enabledPlugins` 键，user 层路径 = `$CLAUDE_CONFIG_DIR`（trim 后非空则用）否则 `~/.claude`，project/local 层为仓库根 `.claude/` 下同名文件；运行一次 `claude plugin list --json`（§9.2 契约），按 §4.1 校验和定位真实 plugin。`Inventory.PluginIDs = installed ∪ settings 已有键`；允许项是否 missing 必须在丢弃 installed 集合前判断。没有 settings 键的自动 plugin 也可能默认开启，必须进入全集 |
| settings | 写 `<session>/claude/settings.json`：`skillOverrides` 对全集逐个写 `on`/`off`，全集 = 扫描到的 Claude 有效名 ∪ settings 已有键 ∪ 投影进来的名字；`enabledPlugins` 对全集逐个写 true/false，全集 = 已安装 ∪ settings 已有键 ∪ 允许列表；`disableBundledSkills = !bundled` |
| 投影 | 外来 skill 复制到 `<session>/claude/addDir/.claude/skills/<basename>/` |
| ControlArgs | `--settings <session>/claude/settings.json`，有投影时加 `--add-dir <session>/claude/addDir` |

启动工厂在解析 executable 和合并 selection 后，将不可变 `claude.Options{Executable, Plugins, Bundled}` 传入 adapter；复制 Plugins，不能通过可变 Inventory 缓存或新增冻结类型字段传选择。插件允许列表控制整个插件，不能靠 skillOverrides 关闭其中单个 skill。`disableBundledSkills` 控制 Claude 的可选 bundled skills；2.1.259 下 `doctor` 等仍可见的内置命令不属于“全部消失”的承诺。允许列表中的未安装项仍写 true 并告警 missing，后续 Claude 自身可能自动安装；skope 不执行 install。

只调用一次 `plugin list`。它不区分「用户开启、依赖强制、managed policy 强制」，因此 skope 不报告依赖闭包和 policy 强制项。这不影响隔离结果：所有非允许 plugin 均写 false，policy 强制项本来就高于 `--settings`。参考实现的三阶段探测约增加 1 s 启动时间，只换来更精确的摘要，v1 不采用。

### 7.2 Codex

| 步骤 | 内容 |
|---|---|
| Inventory | 扫描 Codex 可见目录；读 `$CODEX_HOME/config.toml` 的 `[plugins.*]` 键与 `$CODEX_HOME/plugins` 缓存目录得到 plugin 全集 |
| ControlArgs | `-c 'skills.config=[{path="<abs SKILL.md>",enabled=false},...]'` 列出全集中不在白名单的每个入口；`path` 为绝对路径，skope 先自行 canonicalize 再写入，以匹配 Codex 的 canonicalize 行为；`-c skills.bundled.enabled=<bool>`；对全集中每个不在允许集合的 plugin 写 `-c 'plugins."<id>".enabled=false'` |
| 投影 | 不支持。白名单里 Codex 看不到的 ID 记为 unavailable |

用 `path` 而不用 `name`，避免同名不同目录被一起关闭。Codex 是 denylist 语义，skope 通过「枚举全集再逐条关闭」模拟白名单。扫描到启动之间新增的 skill 存在竞态。

### 7.3 OpenCode

配置文件解析顺序（与 OpenCode 一致，后者覆盖前者）：`~/.config/opencode/opencode.json(c)`、`OPENCODE_CONFIG` 指向的文件、cwd 向上到 worktree 根的每一级 `opencode.json(c)`、`OPENCODE_CONFIG_CONTENT`。支持 jsonc 注释。任一文件存在但解析失败时 fail-closed。

| 步骤 | 内容 |
|---|---|
| Inventory | 扫描 OpenCode 可见的全部目录（含 `.claude`、`.agents` 兼容目录、`$OPENCODE_CONFIG_DIR/skill(s)`）；从上述配置中收集 `skills.paths`（相对路径相对于所属配置文件目录解析）并扫描，Source 记为 `opencode-paths`；检测到 `skills.urls` 时告警不管理；读 `plugin` 数组仅作展示 |
| Env | `OPENCODE_CONFIG_CONTENT`：`permission.skill = {"*":"deny", "<允许有效名>":"allow", ...}`；`skills.paths` = 已收集的用户 `skills.paths` ∪ `<session>/opencode/skills`（有投影时）；`bundled` 为 false 时把内置 `customize-opencode` 写 deny。用户已有 `OPENCODE_CONFIG_CONTENT` 时解析后深合并，skope 的键优先，但 `permission.skill` 除外：该键由 skope 整体接管，不做深合并——skope 只写 `*` 与允许名，深合并会让用户已有的具体 allow 键存活并压过 `*` deny，绕过隔离（Claude 侧无此问题，因为 Claude 把 settings 已有键并进全集逐个写 `off`，OpenCode 无等价手段） |
| 投影 | 外来 skill 复制到 `<session>/opencode/skills/<basename>/` |
| plugin | 不管理 |

`skills.paths` 写成并集是因为 `OPENCODE_CONFIG_CONTENT` 对数组的合并语义未验证（§7.5 第 4 条）；无论替换还是拼接，并集都正确。

### 7.4 有效名与全集的映射

各 adapter 的白名单和 denylist 都使用 `Location.Names[agent]`，而不是 ID。同一 ID 的多个可见 location 的有效名全部进入允许集合。

Claude plugin location 需先通过 `plugins.claude` 允许条件；`skills=["check"]` 不隐式打开 `some-plugin@marketplace`。只存在于未允许 plugin 的 ID 为 unavailable；同 ID 的普通 native 可继续使用。允许 plugin 会暴露它的全部 skills，不宣称实现插件内部逐 skill 隔离。

### 7.5 真实 agent 验证（按 agent 分组阻断）

每项记录 agent 版本、完整命令、可重复的 fixture 目录和结论，写入 `docs/verification.md`。验证按 agent 分组，每组只阻断对应 adapter 所在的实施阶段（§14），不阻断整体：

| 组 | 条目 | 阻断 |
|---|---|---|
| Claude | 3、10 | Phase 2 |
| Codex | 1、6、9 | Phase 3 |
| OpenCode | 2、4、5、7、8 | Phase 4 |

某组结论若与 §3/§4.1/§7 的表述不符，先修订本文档，再开始该组对应的 adapter 实现。Phase 1 只依赖 Claude `--settings` 与 `skillOverrides` 这两项已核实的事实，不等待任何验证。

1. Codex：`codex -c 'skills.config=[{path="/abs/x/SKILL.md",enabled=false}]'` 能被解析且生效；确认 `path` 指向 `SKILL.md` 文件而非目录；symlink 入口下 canonicalize 后是否仍匹配。
2. OpenCode：通过 `OPENCODE_CONFIG_CONTENT` 注入 `skills.paths` 后能发现目录中的 skill；`permission.skill` 中 `*` deny 与具体 allow 的优先级如文档所述；记录当前版本内置 skill 清单（供 §7.3 bundled deny 名单与 §13 边界核对）。
3. Claude：`--add-dir` 目录下 `.claude/skills/<name>` 的投影 skill 能被 `skillOverrides` 的 `on` 打开，且 Claude 看到的名字是 `<name>` 而非带路径前缀。
4. OpenCode：`OPENCODE_CONFIG_CONTENT` 中的数组字段（`skills.paths`、`plugin`）与低层配置合并时是替换还是拼接。
5. OpenCode：项目级 `opencode.json` 中 `permission.skill` 的具体 allow 规则与 `OPENCODE_CONFIG_CONTENT` 层 `*` deny 跨层合并后谁胜出（§7.3 的整体接管方案以此为前提，若具体 allow 胜出则需换方案）。
6. Codex：项目级 `.agents/skills` 是否只沿 cwd 向上扫描到仓库根，还是像 Claude 一样扫描仓库内嵌套子目录；`$CODEX_HOME/skills`、`.codex/skills` 是否同样处理。
7. OpenCode：兼容目录 `.claude/skills`、`.agents/skills` 中嵌套项目目录 skill 的有效名——是否为目录 basename（无作用域前缀），根级与嵌套同名是否冲突。
8. OpenCode：配置文件发现与合并顺序（`~/.config/opencode/opencode.json(c)` → `OPENCODE_CONFIG` → 项目级 → `OPENCODE_CONFIG_CONTENT`），以及 jsonc 注释是否被接受。
9. Codex：plugin ID 的实际格式是否恒为 `name@marketplace` 两段（`skillsets.toml` 校验按此写死）；`-c` 注入的 `skills.config` 数组与用户层 `[[skills.config]]` 合并时是替换还是按 `path` 并集。
10. Claude：plugin 的枚举来源、原地/缓存加载根、自动 plugin 与 namespace（影响 `skope skills`、native 判定与 plugin 全集关闭；结论写入 §4.1）。

## 8. 启动流程与会话目录

### 8.1 流程

```
1.  解析 argv → 子命令。管理命令直接进入对应处理
2.  加载 config.toml。不存在视为空；损坏报错
3.  回收旧会话（§8.2）
4.  解析 agent 可执行文件：配置 command > PATH 查找；Windows 解析 .cmd 垫片到真实 exe 或脚本
5.  决定 skill set：-s 值 > 交互选择器 > 非 TTY 报错
    └ none 或选择器首项 → 不读 skillsets.toml，跳到 9
6.  加载 skillsets.toml，解析并集；校验配置 args 与透传参数无冲突（§6.2）
7.  Adapter.Inventory；解析白名单，划分 native / projected / unavailable / missing
8.  创建会话目录（§8.2），写 owner.json，投影复制（§8.3），Adapter.Plan 生成文件、参数、环境变量
9.  打印摘要与告警
10. 启动（§8.4）
```

### 8.2 会话目录

`~/.skope/sessions/<agent>-<yyyymmdd-hhmmss>-<rand4>/`，权限 `0o700`，含：

- `owner.json`：pid、进程启动时间、agent、skill set 显示名、创建时间
- `<agent>/`：该 agent 的 settings 文件与投影 skill

创建：先在 `sessions/.staging-<rand>/` 下建全所有文件（含 `owner.json`，此时 pid 为 skope 自身），再 `os.Rename` 到最终名。Unix 上 exec 保持 PID，因此 owner 不需要更新。

回收：每次启动遍历 `sessions/`。跳过 `.staging-*`（除非修改时间超过 1 小时）；读 `owner.json`，pid 不存在或进程启动时间不匹配则整体删除。没有 `owner.json` 的目录视为残留，修改时间超过 1 小时才删除。回收失败只告警。

Windows 上正常退出立即清理；崩溃残留由回收兜底。Windows 不支持 POSIX 权限位，`0o700`/`0o600` 只在 Unix 生效；Windows 依赖 `%USERPROFILE%` 下默认 DACL（仅当前用户与管理员可访问），文档写明。

### 8.3 投影复制契约

对选定的 location 目录：

1. `DiscoveryPath`/`RealPath` 均指向 `SKILL.md`，复制根取 `Dir(DiscoveryPath)` 解析后的目录，不把 `SKILL.md` 文件自身当复制根。入口 symlink/Junction 可解析后检查。
2. 递归检查目录。内部 symlink 的目标仍在复制根内则复制目标内容；否则整个 skill 为 unavailable。链接环、特殊文件及含改变 agent 加载语义的 plugin manifest 目录不可投影。
3. 上限：2000 个文件或 20 MiB，超出判定 `unavailable`。
4. 文本启发式：扫描 `.md` 文件中的 `../`、`${CLAUDE_PLUGIN_ROOT}`、`${CODEX_PLUGIN_ROOT}` 与绝对路径引用，命中只告警「可能引用外部文件，投影后可能不完整」，不阻断。
5. 复制后的文件权限 `0o600`，目录 `0o700`。
6. 检查阶段记录路径、大小及内容摘要，复制前复查；检查后源内容改变时失败并清理 staging，不能发布部分内容。真实读写错误 fail-closed。
7. skill 内文件/目录路径逐组件按 `strings.EqualFold` 检查别名和文件/目录冲突；允许共享完全同名父目录，拒绝 `Refs/a.md` 与 `refs/b.md` 等别名。写文件必须使用锚定 staging 的独占创建（`O_CREATE|O_EXCL`），不能 O_TRUNC、先删再写或先 Stat 再非独占创建；已有目标导致整次会话失败且原内容保持不变。

### 8.4 进程启动

- Unix：`syscall.Exec(path, argv, env)`。失败报错退出码 1。
- Windows：skope 启动时先把自身加入一个新建的 Job Object，设 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 且不允许 breakaway；之后 `os/exec` 派生的 agent 自动成为该 Job 成员，没有「启动后再分配」的竞态窗口。skope 已处于不允许嵌套的外层 Job 时告警并降级为无 Job 启动。stdin/stdout/stderr 继承；skope 忽略 Ctrl+C，由控制台直接送达子进程；等待后透传退出码，退出后删除会话目录。
- Windows 垫片解析：npm 安装的 `claude`、`codex`、`opencode` 是 `.cmd` 文件。skope 读取 `.cmd` 内容找到真实 `.exe` 或 node 脚本并直接启动，避免 cmd.exe 的「终止批处理操作吗」提示。找不到时回退 `cmd.exe /c` 并告警。

## 9. 输出安全与子进程契约

### 9.1 输出安全

- 所有来自外部的文本（路径、skill 描述、frontmatter、配置字段、子进程 stderr）在写入终端前转义 C0/C1 控制字符、`؜`、`‎`、`‏`、` `、` `、`‪`-`‮`、`⁦`-`⁩`，形式为 `\xNN` 或 `\uNNNN`。
- 环境变量值脱敏：键名（大小写不敏感）含 `token`、`key`、`secret`、`password`、`auth`、`header`、`credential` 的显示为 `<redacted>`；`OPENCODE_CONFIG_CONTENT` 只显示 skope 注入的键。
- 冲突参数只打印参数名与来源。
- 子进程 stderr 进入错误信息时截断到 2 KiB 并转义。

### 9.2 子进程契约

适用于 `claude plugin list --json`、`doctor` 的版本探测：

- 超时 15 s，超时后杀进程树（Unix 进程组、Windows Job），报错含命令名与超时时长。
- stdout/stderr 各上限 4 MiB，超出即终止并报错「输出超限」。
- 为子进程创建的临时文件随机名、`0o600`，无论成败都清理；清理失败只告警。
- stdin 关闭；继承环境但不注入 skope 自己的会话变量。

## 10. 错误处理

| 场景 | 行为 | 退出码 |
|---|---|---|
| agent 可执行文件找不到 | 报错并提示配置 `[agents.x] command` | 1 |
| `config.toml` 损坏 | 报错，含 `-s none` | 1 |
| `skillsets.toml` 不存在，未传 -s | 选择器只有「不隔离」一项 | 沿用 agent |
| `skillsets.toml` 损坏，未传 -s（含 TTY 与非 TTY） | fail-closed，报文件路径与解析错误 | 1 |
| `skillsets.toml` 不存在或损坏，显式 -s <name> | 报错并给出路径 | 1 |
| -s none 且 `skillsets.toml` 损坏 | 不读该文件，正常启动 | 沿用 agent |
| -s 名字不存在 | 报错并列出可用名 | 1 |
| 扫描 ENOENT | 跳过 | 沿用 agent |
| 扫描 EACCES 等其他 I/O 错误 | fail-closed | 1 |
| `SKILL.md` frontmatter 已起始但 YAML 非法、缺结束分隔符或 `name` 非字符串 | fail-closed，报错含 `SKILL.md` discovery path | 1 |
| OpenCode 配置文件存在但解析失败 | fail-closed | 1 |
| 已有 `OPENCODE_CONFIG_CONTENT` 非法 JSON | fail-closed | 1 |
| `claude plugin list` 失败、超时、超限或 JSON 非法 | fail-closed | 1 |
| 白名单 ID 对本 agent unavailable | 告警继续 | 沿用 agent |
| 白名单 ID 全集中不存在 | 告警，missing | 沿用 agent |
| 允许的 plugin 未安装 | 告警，missing | 沿用 agent |
| 配置 args 或透传参数冲突 | 报错，参数名与来源 | 1 |
| 投影自包含检查失败 | 该 ID unavailable，告警继续 | 沿用 agent |
| 会话目录创建或复制失败 | 报错，删除 staging 目录 | 1 |
| 旧会话回收失败 | 告警继续 | 沿用 agent |
| 非 TTY 未传 -s | 报错 | 1 |
| 选择器取消 | 静默退出 | 0 |
| create/edit 非 TTY | 报错 | 1 |
| 写入时检测到并发修改 | 报错，保留原文件 | 1 |
| 写入失败 | 保留原文件，报错 | 1 |
| Unix exec 失败 | 报错 | 1 |
| Windows Job 不可用 | 告警，无 Job 启动 | 沿用 agent |

## 11. 目录结构

模块路径 `github.com/scarb/skope`。全部代码在 `internal/` 下，不提供公共库 API。

```
skope/
├── cmd/
│   └── skope/
│       └── main.go              # 唯一 main 包：接收 ldflags 版本，调用 cli.Execute，os.Exit
├── internal/
│   ├── cli/                     # cobra 命令定义与参数解析，不含业务逻辑
│   │   ├── root.go
│   │   ├── launch_cmd.go        # claude/codex/opencode 启动子命令（DisableFlagParsing）
│   │   ├── args.go              # -s/--set、--dry-run、-- 切分（§6.1）
│   │   ├── conflict.go          # 冲突参数检测（§6.2）
│   │   ├── manage_cmd.go        # list/create/edit/delete/skills/doctor
│   │   ├── render.go            # §6.3 dry-run 与 §6.6 摘要渲染
│   │   └── integration_test.go  # fake agent 端到端
│   ├── launch/                  # §8.1 启动流程编排（用例层），只依赖接口
│   ├── agent/                   # Adapter/Capabilities/LaunchPlan/Inventory 与显式注册表
│   │   ├── agent.go
│   │   ├── registry.go
│   │   ├── claude/              # §7.1
│   │   │   ├── adapter.go
│   │   │   ├── inventory.go     # 三层 settings 与 claude plugin list --json
│   │   │   ├── settings.go      # settings.json 生成
│   │   │   └── testdata/        # golden settings.json、plugin list fixture
│   │   ├── codex/               # §7.2
│   │   │   ├── adapter.go
│   │   │   ├── inventory.go
│   │   │   ├── args.go          # -c 参数序列
│   │   │   └── testdata/
│   │   └── opencode/            # §7.3、§7.4
│   │       ├── adapter.go
│   │       ├── configfile.go    # jsonc 搜索顺序与解析
│   │       ├── content.go       # OPENCODE_CONFIG_CONTENT 生成与深合并
│   │       └── testdata/
│   ├── skill/                   # §4：扫描、身份、有效名、可见性判定
│   │   ├── skill.go             # Skill/Location 与枚举类型
│   │   ├── scope.go             # §4.1 扫描范围表（数据驱动）
│   │   ├── scan.go              # 遍历规则
│   │   ├── identity.go          # ID、同 ID 合并、碰撞
│   │   ├── resolve.go           # §4.4 native/projected/unavailable/missing
│   │   └── testdata/
│   ├── config/                  # §5
│   │   ├── config.go            # config.toml
│   │   ├── skillsets.go         # skillsets.toml 与 §5.5 并集
│   │   ├── validate.go
│   │   ├── write.go             # §5.4 CAS 原子写与有序序列化
│   │   ├── home.go              # SKOPE_HOME 解析
│   │   └── errors.go            # 路径化错误
│   ├── session/                 # §8.2 会话目录、owner.json、回收
│   ├── projection/              # §8.3 投影复制契约
│   │   ├── copy.go
│   │   └── check.go             # symlink 边界、文件数与体积上限、文本启发式
│   ├── handoff/                 # §8.4 把控制权交给 agent 进程
│   │   ├── handoff.go
│   │   ├── handoff_unix.go      # syscall.Exec
│   │   ├── handoff_windows.go
│   │   ├── job_windows.go       # Job Object
│   │   └── shim_windows.go      # npm .cmd 垫片解析
│   ├── proc/                    # §9.2 辅助子进程契约
│   ├── termsafe/                # §9.1 转义与脱敏
│   ├── host/                    # 进程环境快照（env、home、cwd），唯一读取 os.Environ 的地方
│   ├── wizard/                  # §6.5 向导编排
│   │   ├── wizard.go            # 候选构造、过滤、校验、摘要、写入编排
│   │   ├── prompter.go          # Prompter 接口
│   │   └── huh.go               # huh 实现
│   ├── doctor/                  # §6.4 doctor 检查项
│   └── testutil/
│       ├── fakeagent/
│       │   └── main.go          # 记录 argv/env/cwd 为 JSON 后按指定码退出
│       └── build.go             # TestMain 中把 fakeagent 编译到临时目录
├── docs/
├── .github/workflows/ci.yml
├── .goreleaser.yaml
├── .golangci.yml
├── go.mod
└── go.sum
```

各包的 `_test.go` 与被测代码同目录，树中省略。

### 11.1 依赖方向

```
cli          → launch, wizard, doctor, config, agent, agent/claude, agent/codex, agent/opencode
launch       → agent, skill, config, session, projection, handoff, host, termsafe
agent/<name> → agent, skill, session, proc, host, termsafe
agent        → skill, session
wizard       → config, skill, agent, host
doctor       → config, host, handoff
skill        → host
config       → host
叶子         : session, projection, handoff, proc, termsafe, host
```

禁止：任何包 import `cli` 或 `launch`；`agent/<name>` 之间互相 import；叶子包 import `agent`。`go vet` 不检查这一点，用 `golangci-lint` 的 `depguard` 规则固化。

§7 接口中的类型对应：`Env` 即 `host.Env`，`Resolved` 即 `skill.Resolved`，`Session` 即 `session.Session`。

### 11.2 约定

- `cmd/skope` 只有 `main.go`，不放业务逻辑。版本号用 `-ldflags "-X main.version=..."` 注入，传给 `cli.Execute`。
- 包名短、小写、单个词、无下划线；不设 `util`/`common`/`helpers` 包。`skill.Skill` 的重名与 `context.Context` 同类，接受。
- 接口在消费方定义。`Adapter` 定义在 `internal/agent`，实现在子包；注册表由 `cli` 显式构造并传入，禁止 `init()` 副作用注册。
- I/O 全部注入：文件系统走 `fs.FS`（写入走 `config`/`session` 自定义的最小接口），环境走 `host.Env`，子进程走 `proc.Runner`。`skill`、`config`、`projection` 的核心逻辑不 import `os`。
- 平台差异放在 `handoff`、`session`、`proc` 以及 `host` 的普通文件打开/路径检查专用 `_unix.go`/`_windows.go` 文件（`//go:build unix` 与文件名后缀）；通用业务文件不混写 GOOS 分支。
- 每个包用 sentinel 或类型化错误暴露可判定的失败，调用方用 `errors.Is`/`errors.As`；包装用 `%w`；§10 的用户可见文案只在 `cli` 拼装。
- 数据结构按值传递或返回新副本，不就地修改入参（仓库根 CLAUDE.md 不可变要求）。
- 单元测试用 `package xxx_test` 黑盒风格，只测 §12 列出的 seam；golden 文件与 fixture 放各包 `testdata/`，用 `-update` flag 重生成。集成测试在 `internal/cli`，`testing.Short()` 时跳过。
- 文件 200 到 400 行，单一职责；超过 800 行必须拆分。
- 实施分期见 §14。

## 12. 测试策略

- 单元测试
  - `internal/skill`：`testing/fstest.MapFS` 覆盖每种目录布局、嵌套作用域 ID、command 与 skill 同 ID 合并、Junction 双入口不去重、`RealPath` 同目标标注、frontmatter 名不一致、有效名碰撞告警、ENOENT 跳过、EACCES fail-closed；symlink 用真实临时目录。
  - `internal/config`：两个文件的全部校验规则、路径化错误、CAS 写入检测到并发修改、写失败保留原文件、并集。
  - `internal/cli`：参数切分、`-s` 组合与重复、`--` 处理、配置 args 与透传参数冲突检测（含 `--flag=value` 形式与 Codex `-c skills.*`/`-c=...` 取值形式）、非 TTY 行为、`-s none` 不读 `skillsets.toml`。
  - 三个 adapter：golden file 断言 settings JSON、`-c` 参数序列、`OPENCODE_CONFIG_CONTENT` JSON；OpenCode 配置文件搜索与 `skills.paths` 收集；已有 `OPENCODE_CONFIG_CONTENT` 深合并；有效名映射。
  - `internal/session`：staging 发布、owner 判定（已退出 PID 与当前 PID）、回收跳过规则。
  - `internal/projection`：symlink 边界与自包含检查的每个分支、文件数与体积上限、文本启发式告警、权限位。
  - `internal/launch`：§8.1 十步编排，注入 fake adapter、内存 fs 与记录型 handoff，断言步骤顺序、失败时的会话目录清理、`--dry-run` 不触发 handoff。
  - `internal/proc`：超时、输出超限、kill 失败、临时文件清理失败。
  - `internal/termsafe`：控制字符转义、脱敏键匹配。
  - `internal/wizard`：向导编排逻辑（候选构造与过滤、名字校验、摘要生成、写入编排）通过 `Prompter` 接口与 huh 分离，测试注入脚本化 Prompter；编排逻辑全部可单测。
  - `internal/handoff`：Windows `.cmd` 垫片解析（fixture 垫片文件）、嵌套 Job 降级判定；Unix exec 路径在集成测试中覆盖。
- 集成测试（`internal/cli`）：`TestMain` 把 `internal/testutil/fakeagent` 编译到临时目录，配置 `command` 指向它，端到端验证启动、argv 顺序、透传、退出码、会话目录创建与清理。Windows 上额外验证 `.cmd` 垫片解析与 Job Object 继承。
- 真实 agent 验证与手工验收：§7.5 各条结论与各期的手工验证结论写入 `docs/verification.md`，记录 agent 版本，agent 大版本升级后重跑。
- 目标覆盖率 80%，CI 运行 `go test -race ./...`（ubuntu/macos）、`go vet`、`golangci-lint`。

## 13. 已知边界

- 不是安全边界，用户可绕过 skope 直接启动 agent。
- Claude managed policy 高于 `--settings`，skope 不检测也不覆盖 policy 强制启用的 plugin。
- Claude 允许的 plugin 的依赖由 Claude 自动启用，skope 不报告依赖闭包。
- Codex 为 denylist 模拟白名单，扫描到启动之间新增的 skill 存在竞态。
- `--add-dir` 会同时授予该目录文件访问权，会话目录中只有 skill 副本。
- 三个 agent 的 skill 命名、plugin 列表输出和配置键都不是 skope 控制的稳定协议，解析失败一律 fail-closed。
- 投影自包含检查是结构性的，文本引用只告警；引用外部文件的 skill 投影后可能不完整。
- plugin 内 skill 的跨 agent 投影是 v1 之后的第一个增量：模型已通过 `Location.Level == plugin` 预留，实现时增加 plugin skill 枚举与撞名策略（跳过并告警，不自动改名），自包含检查复用 §8.3。
- OpenCode plugin 与 `skills.urls` 不受管理。
- OpenCode `bundled=false` 只 deny 已知内置清单（`customize-opencode`）；OpenCode 新增内置 skill 不在其中，清单以 §7.5 第 2 条的记录为基线，agent 升级后需复核。
- Unix 上 exec 后 PID 属于 agent；agent 若 fork 出守护进程后主进程退出，PID 消失，下次启动会把仍在使用的会话目录误判为残留删除。这是 exec 模式的固有代价，Windows（skope 等待并清理）无此问题。
- `skope skills` 需要运行 `claude plugin list`（约 1 s）才能列出 plugin 级 location；缓存与否由实现决定，文档不承诺。
- 不记忆上次选择，不支持项目级配置。
- Windows 上文件权限依赖用户目录默认 DACL。

## 14. 实施分期

按 agent 纵切，每期结束都有一条可运行的 `skope` 命令。理由：本项目的不确定性集中在与真实 agent 的交界处（§3 的管控事实、§7.5 的待验证项），纵切让 §4 身份模型、§7 Adapter 接口和 §8 会话目录在第一期就经过真实链路检验；按包横切并行开发会把全部集成风险推到最后。

| Phase | 交付 | 前置验证（§7.5） | 依赖 |
|---|---|---|---|
| 0 | 项目准备：模块、cobra 根命令与 `version`、fakeagent 与 `BuildFakeAgent`、golangci-lint v2 与 depguard 分层规则、三平台 CI、goreleaser 骨架、Makefile（`make check`）、`docs/verification.md` 模板 | 无 | 无 |
| 1 | Claude 最小闭环（Unix） | 无 | Phase 0 |
| 2 | Claude 补全 | 第 3、10 条 | Phase 1 |
| 3 | Codex adapter | 第 1、6、9 条 | Phase 2 |
| 4 | OpenCode adapter | 第 2、4、5、7、8 条 | Phase 2 |
| 5 | 管理面 | 无 | Phase 3、4 |
| 6 | Windows 与发布 | 无 | Phase 2，可与 3、4 并行 |

Phase 3 与 Phase 4 互不依赖，可并行。

### 14.1 Phase 1：Claude 最小闭环

目标：`skope claude -s <set>` 在 Unix 上按 skill 白名单启动真实 Claude。

范围：

- `internal/skill`：§4.1 扫描范围表实现为数据驱动，本期只启用 Claude 可见的全局与项目级行（含 `.claude/commands` 与嵌套作用域），plugin 级留待 Phase 2；§4.3 ID、同 ID 合并、有效名、碰撞告警；§4.4 判定只走 `native`/`missing` 两个分支。
- `internal/config`：`config.toml` 与 `skillsets.toml` 只读加载、§5.2/§5.3 全部校验、§5.5 并集、`SKOPE_HOME` 解析。不含写入。
- `internal/agent` 与 `internal/agent/claude`：§7 全部类型定义；Claude adapter 只实现 `skillOverrides`（全集 = 扫描到的有效名 ∪ settings 已有键）与 `--settings`。
- `internal/session`：§8.2 staging 创建、`owner.json`、回收。不含投影。
- `internal/host`：环境快照。
- `internal/handoff`：Unix `syscall.Exec`。
- `internal/launch`：§8.1 流程编排，本期跳过投影相关步骤。
- `internal/cli`：`skope claude` 的 `-s`/`--set`（含 `none`、重复报错、逗号并集）、`--dry-run`、`--` 透传切分、非 TTY 未传 `-s` 报错、§6.1 argv 顺序；`list`。
- `internal/cli` 集成测试（§12），使用 Phase 0 提供的 `testutil.BuildFakeAgent`，本期起每期沿用。

不含：plugin、bundled、投影、TTY 选择器、§6.2 冲突参数检测、§6.6 完整摘要（只打印 native/missing 计数）、§9 输出安全。

退出标准：fake agent 集成测试覆盖 argv 顺序、settings.json 内容、会话目录创建与回收、§10 中本期涉及的错误行；真实 Claude 手工验证白名单生效并记录于 `docs/verification.md`。

冻结点：本期结束冻结 `Skill`、`Location`、`Adapter`、`Capabilities`、`LaunchPlan`、`Inventory` 的类型定义。此后改动先改本文档再改代码。

### 14.2 Phase 2：Claude 补全

前置：§7.5 第 3、10 条通过。

范围：

- `internal/proc`（§9.2）；`claude plugin list --json` 解析与校验；`enabledPlugins` 全集逐项写入；`disableBundledSkills`；§5.3 `plugins.claude` 与 `bundled` 生效；扫描范围表启用 Claude plugin 行。
- 提前启用 `~/.agents/skills` 与 `$CODEX_HOME/skills` 两条全局外来扫描行，仅供 Claude 投影；Names 按来源填写，不注册 Codex/OpenCode adapter。其他 Codex 项目/admin/plugin 与 OpenCode 配置扫描仍留后续阶段。
- `internal/projection`：§8.3 复制契约与自包含检查；`launch` 接入投影步骤，`<session>/claude/addDir`，`--add-dir`；§4.4 `projected`/`unavailable` 分支。
- §6.2 冲突参数检测（Claude 行）。
- §6.6 摘要；`internal/termsafe`（§9.1）接入全部外部文本输出。
- §6.3 `--dry-run` 输出字段补齐。

退出标准：Claude 达到 §7.1 全部要求；§10 中 Claude 相关行全部有测试；golden file 断言 settings.json。

### 14.3 Phase 3：Codex adapter

前置：§7.5 第 1、6、9 条通过；结论与 §3/§4.1/§7.2 不符先修订本文档。

范围：

- 复用 Phase 2 已提前启用的 `~/.agents/skills`、`$CODEX_HOME/skills` 全局行，补齐其余 Codex 行（项目 `.agents/skills`、`.codex/skills`、`/etc/codex/skills`、plugin 缓存）。
- `internal/agent/codex`：`$CODEX_HOME/config.toml` 的 `[plugins.*]` 与缓存目录组成 plugin 全集；`-c skills.config` denylist（canonicalize 后的绝对 `SKILL.md` 路径）；`skills.bundled.enabled`；plugin 关闭参数。
- 启用 Codex 目标的跨 agent 可见性：`Capabilities.Projection = false` 对应的 `unavailable` 分支与原因文案；§5.3 `plugins.codex` 生效。Claude 目标的外来投影已在 Phase 2 启用。
- §6.2 冲突参数检测（Codex `-c skills.*`/`plugins.*`，含 `-c=...` 取值形式）。

退出标准：golden file 断言 `-c` 参数序列；fake agent 集成测试；真实 Codex 手工验证 denylist 生效与 symlink 入口 canonicalize 匹配。

### 14.4 Phase 4：OpenCode adapter

前置：§7.5 第 2、4、5、7、8 条通过。第 5 条若为「具体 allow 胜出」，§7.3 的 `permission.skill` 整体接管方案不成立，先修订本文档再实现。

范围：

- 扫描范围表启用 OpenCode 行（含 `.claude`/`.agents` 兼容目录、`$OPENCODE_CONFIG_DIR`、`skills.paths`）。
- `internal/agent/opencode`：§7.3 配置文件解析顺序（jsonc、fail-closed）、`skills.paths` 收集、`skills.urls` 告警、`OPENCODE_CONFIG_CONTENT` 生成、与用户已有值的深合并（`permission.skill` 整体接管）、bundled deny 清单。
- 投影到 `<session>/opencode/skills`，复用 §8.3。
- §7.4 有效名映射（frontmatter `name` 回退目录名）；§4.3 「不同 ID 同有效名」碰撞在 OpenCode 上的告警。

退出标准：golden file 断言 `OPENCODE_CONFIG_CONTENT`；fake agent 集成测试；真实 OpenCode 手工验证 `*` deny 加具体 allow 与注入目录生效。

### 14.5 Phase 5：管理面

前置：Phase 3、4 完成。`skills` 与向导候选需要三个 agent 的 Inventory 都稳定；前四期用手写 `skillsets.toml` 即可。

范围：

- §6.1 TTY 选择器（含「不隔离」首项、取消退出码 0）。
- §5.4 CAS 原子写与有序序列化。
- §6.5 `create`/`edit`/`delete` 向导；`internal/wizard` 编排逻辑与 huh 组件分离（§12）。
- §6.4 `skills`（含 `--agent`、碰撞告警）；`internal/doctor` 与 `doctor` 命令（含 conhost 提示）。

退出标准：§6.4 契约表每一行的行为、非 TTY 行为与退出码均有测试；向导编排逻辑单测覆盖。

### 14.6 Phase 6：Windows 与发布

前置：Phase 2。可与 Phase 3、4 并行。

范围：

- `internal/handoff` Windows 文件（§8.4）：Job Object（`KILL_ON_JOB_CLOSE`、嵌套 Job 降级告警）、Ctrl 处理、退出码透传与退出后清理、`.cmd` 垫片解析与 `cmd.exe /c` 回退。
- §8.2 Windows 权限说明；`doctor` 的垫片真实目标展示。
- goreleaser 配置、Homebrew tap、Scoop bucket；CI 三平台（`-race` 仅 ubuntu/macos，见 §14.7）`go test`、`go vet`、golangci-lint。

退出标准：Windows 集成测试（垫片解析、Job 继承）通过；三平台 release 产物可安装运行。

### 14.7 分期规则

- 每期内按 TDD 推进，`make check`（`gofmt`、`go vet`、`golangci-lint`、`go test ./...`、`go build`）本地全绿，且 CI 三平台通过（ubuntu/macos 带 `-race`，Windows 不带，因 `-race` 依赖 cgo）才进入下一期。
- Phase 1 结束后类型冻结。Phase 3、4 是唯一允许调整 `Adapter` 接口的节点，调整必须先修订 §7 再改代码，并回归 Claude adapter 的测试。
- 各期范围只增不减：某项在本期被砍，必须挪入后续某期，不得从本文档消失。
- `docs/verification.md` 随各期累积：每期的真实 agent 手工验证结论追加进去，不另建文件。
