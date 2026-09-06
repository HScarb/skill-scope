# 真实 agent 验证记录

对应 spec §7.5。每条记录 agent 版本、完整命令、可重复的 fixture 目录布局和结论。
结论与 spec §3/§4.1/§7 不符时，先修订 spec 再开始对应 adapter 实现。

约束：绝不修改用户真实的 `~/.claude`、`~/.codex`、`~/.config/opencode`。
实验全部通过 `CLAUDE_CONFIG_DIR`、`CODEX_HOME`、`OPENCODE_CONFIG_DIR`、
`OPENCODE_CONFIG_CONTENT` 重定向到临时 fixture 目录。

状态取值：`未验证` / `通过` / `不符（已修订 spec §x）`。

## Claude（Phase 2 前置验证）

### 第 3 条：`--add-dir` 投影 skill 可被 `skillOverrides` 打开

- 状态：通过（2026-09-05）。
- agent 版本：`2.1.259 (Claude Code)`。
- 平台：Windows 11，`Microsoft Windows NT 10.0.26200.0`，PowerShell 7.6.5；直接执行 Windows PE，不经过 WSL 或 skope handoff。
- 隔离：子进程的 `HOME`、`USERPROFILE`、`CLAUDE_CONFIG_DIR`、`SKOPE_HOME` 分别指向 fixture 的 `home`、`home`、`claude`、`skope`；cwd 为 `fixture/repo`。仅把现有 provider 所需的 `ANTHROPIC_*`、代理及 TLS 环境值注入子进程内存；没有复制真实 settings、skills、hooks 或 plugin。stdin 关闭，stdout/stderr 分开收集。

```text
fixture/                                      # 随机临时目录，绝对路径见下
  home/
  claude/
  skope/
  repo/.git/
  addDir/.claude/skills/projected-check/
    SKILL.md
    references/marker.md
  on.json
  off.json
```

`SKILL.md` 的完整内容如下，marker 只存在于资源文件中：

```markdown
---
name: different-frontmatter-name
description: Controlled projection verification fixture
---
Read the file references/marker.md relative to this skill's directory using the Read tool, then output its exact contents and nothing else. Do not guess the contents.
```

`references/marker.md` 为 `SKOPE_ADDDIR_RESOURCE_a18a7799` 加换行；`on.json` 为 `{"skillOverrides":{"projected-check":"on"}}`，`off.json` 为 `{"skillOverrides":{"projected-check":"off"}}`。

以下 PowerShell 命令均使用上述子进程环境与 cwd；provider 值不出现在命令参数、fixture 或本文中：

```powershell
$CLAUDE_EXE = 'D:/programs/scoop/apps/nodejs-lts/current/bin/node_modules/@anthropic-ai/claude-code/bin/claude.exe'
$FIXTURE = 'C:/Users/j00466872/AppData/Local/Temp/skope-phase2-adddir-a18a7799'
& $CLAUDE_EXE --version
& $CLAUDE_EXE --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/on.json" -p /projected-check --max-turns 2 --output-format text
& $CLAUDE_EXE --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/off.json" -p /projected-check --max-turns 2 --output-format text
& $CLAUDE_EXE --settings "$FIXTURE/on.json" -p /projected-check --max-turns 2 --output-format text
& $CLAUDE_EXE --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/on.json" -p /different-frontmatter-name --max-turns 2 --output-format text
& $CLAUDE_EXE --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/off.json" -p /different-frontmatter-name --max-turns 2 --output-format text
& $CLAUDE_EXE --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/on.json" -p /projected-check --max-turns 2 --verbose --output-format stream-json --allowedTools Read
& $CLAUDE_EXE --add-dir "$FIXTURE/addDir" --settings "$FIXTURE/on.json" -p /different-frontmatter-name --max-turns 2 --verbose --output-format stream-json --allowedTools Read
```

| 检查 | 观察 |
|---|---|
| basename + on | 输出 `SKOPE_ADDDIR_RESOURCE_a18a7799` |
| basename + off | CLI 确定性报告 `Skill "projected-check" is disabled via skillOverrides. Remove the override from your settings to run it.`；没有 marker |
| 没有 add-dir | CLI 报告 `Unknown command: /projected-check`；没有 marker |
| frontmatter 名 + basename on | 同样输出 marker，说明该版本也接受 frontmatter 名作为调用别名 |
| frontmatter 名 + basename off | 同样报告 `Skill "projected-check" is disabled via skillOverrides...`；别名没有绕过 basename 的关闭规则 |
| 两个 stream-json on 组 | `system/init.skills` 和 `slash_commands` 都包含 `projected-check`，不包含 `different-frontmatter-name`；`plugins` 为空 |
| 资源读取 | 两个 stream-json 组均出现 `Read` 的 `tool_use`，`file_path` 指向 `addDir/.claude/skills/projected-check/references/marker.md` 的绝对路径；对应 `tool_result` 和 `tool_use_result.file.content` 返回唯一 marker，最终 `result` 也为该 marker，`permission_denials` 为空 |

所有调用退出码均为 0，因此通过依据是 CLI 的明确拒绝、初始化发现列表和实际 Read 工具结果，不是退出码或模型自述。basename 与 frontmatter alias 的初始化列表一致；白名单控制键仍是 basename `projected-check`，无需为别名另写 override。本实验支持 spec §7.5 第 3 条；不证明原生 Linux/macOS Claude 行为，也不覆盖同名别名间的优先级。

复现实验 harness 暂存于 `C:/Users/j00466872/AppData/Local/Temp/skope-phase2-task1.ps1`，只保存注入逻辑及上述无秘密 fixture；实际 provider 值在每次运行时读入进程内存。fixture 与脱敏输出暂留供独立复核；检查 29 个 fixture 文件未发现现用 `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_API_KEY` 值，完成复核后清理。该临时 harness 和输出不纳入产品提交。

### 第 10 条：plugin 缓存目录中 skill 的枚举来源与布局

- 状态：不符（2026-09-05，已修订 spec §4.1/§7.1：不能只扫描 cache；门禁通过）。
- agent：Claude Code `2.1.259`，Windows 11、PowerShell 7.6.5，与第 3 条同一绝对 PE。
- 隔离根：`C:/Users/j00466872/AppData/Local/Temp/skope-phase2-plugin-09fc42ab`（以下 `$F`）；harness：`C:/Users/j00466872/AppData/Local/Temp/skope-phase2-task2.ps1`。HOME/USERPROFILE=`$F/home`，CLAUDE_CONFIG_DIR=`$F/claude`，SKOPE_HOME=`$F/skope`，默认 cwd=`$F/repo`（有空 `.git`）。provider 仅在模型调用时按第 3 条方式注入内存，输出只替换具体凭据/URL，不替换 `maxOutputTokens` 等数字。
- 每次子进程 stdin EOF、超时 60 秒、stdout/stderr 分开保存。以下 CLI 命令均通过上述隔离 harness 执行；实验没有连接公网 marketplace。

```text
fixture/
  home/  claude/  skope/
  repo/.git/  repo/sub/  other/.git/
  market/.claude-plugin/marketplace.json
  market/{allowed-plugin,blocked-plugin,uninstalled-plugin}/
    .claude-plugin/plugin.json
    skills/check/SKILL.md
  claude/skills/personal-auto/.claude-plugin/plugin.json
  claude/skills/personal-auto/skills/check/SKILL.md
  repo/.claude/skills/project-auto/.claude-plugin/plugin.json
  repo/.claude/skills/project-auto/skills/check/SKILL.md
  git-market/                         # 本地 git 仓库，cache-plugin/ 布局同上
  claude/plugins/marketplaces/skope-git-fixture/ # 从本地 git clone
  allow.json  override.json  bundled.json  auto-off.json  project-only.json  cache-only.json
```

marketplace JSON 为 `{"name":"skope-fixture","owner":{"name":"skope-test"},"plugins":[{"name":"allowed-plugin","source":"./allowed-plugin"},{"name":"blocked-plugin","source":"./blocked-plugin"},{"name":"uninstalled-plugin","source":"./uninstalled-plugin"}]}`；各 manifest 为 `{"name":"<目录名>","version":"1.0.0"}`。普通 plugin 的 SKILL.md frontmatter 为 `name: different-check-name`、`description: Controlled plugin verification`，正文要求仅返回 `SKOPE_<大写插件名，下划线替换连字符>_09fc42ab`。自动 plugin 的 frontmatter name 为 check，正文 marker 为 `AUTO_PLUGIN_09fc42ab`。

```powershell
& $CLAUDE_EXE plugin list --help
& $CLAUDE_EXE plugin marketplace add "$F/market" --scope user
& $CLAUDE_EXE plugin install allowed-plugin@skope-fixture --scope user
& $CLAUDE_EXE plugin install blocked-plugin@skope-fixture --scope user
& $CLAUDE_EXE plugin list --json
& $CLAUDE_EXE plugin install allowed-plugin@skope-fixture --scope project
& $CLAUDE_EXE plugin install allowed-plugin@skope-fixture --scope local
& $CLAUDE_EXE plugin list --json                  # 分别在 repo、other、repo/sub 执行
& $CLAUDE_EXE plugin list --json --available
```

| 对照/输出文件前缀 | 实际观察 |
|---|---|
| list-user / list-multi | 顶层数组；id/string、enabled/bool、scope/string、installPath/绝对字符串。相同 ID 的 user/project/local 是三行；project/local 另有 projectPath。version、installedAt、lastUpdated 是非关键字段 |
| list-other / list-nested | 列表仍保留 repo 的 project/local 安装；不能把全部行直接当 cwd 有效位置，也不能过滤后判安装 missing |
| list-default | 从所有 settings 删除 blocked-plugin 键后，该普通安装仍在列表，enabled=false；没有证明普通市场插件默认启用 |
| list-available | 只有加 --available 才变 `{installed,available}`，未安装 uninstalled-plugin 只在 available；正式 probe 禁止该选项 |
| list-auto | personal-auto@skills-dir 原地加载、scope=user、enabled=true、没有 settings 键；确有默认开启项。未信任 project 输出 `(suppressed)@skills-dir` 占位，空 installPath、enabled=false、version=unknown、notes 字符串数组 |
| list-trusted-forward | 仅 fixture `.claude.json` 的 `projects[正斜杠绝对repo].hasTrustDialogAccepted=true` 后，list 列出 project-auto@skills-dir，scope=project、原地 installPath、无 projectPath。该步骤是预置隔离信任状态，不是测试交互信任 UI |
| list-nested | cwd=repo/sub 不扫描 repo 的自动 plugin；只检查 cwd 自身，普通安装记录仍全列 |

模型可见性调用统一后缀为 `--max-turns 1 --verbose --output-format stream-json`，完整参数如下；前两个对照在创建自动插件前执行：

```powershell
& $CLAUDE_EXE --settings "$F/allow.json" -p /allowed-plugin:check --max-turns 1 --verbose --output-format stream-json
& $CLAUDE_EXE --settings "$F/override.json" -p /allowed-plugin:check --max-turns 1 --verbose --output-format stream-json
& $CLAUDE_EXE --settings "$F/allow.json" -p /blocked-plugin:check --max-turns 1 --verbose --output-format stream-json
& $CLAUDE_EXE --settings "$F/bundled.json" -p 'Reply OK only.' --max-turns 1 --verbose --output-format stream-json
& $CLAUDE_EXE --settings "$F/auto-off.json" -p /personal-auto:check --max-turns 1 --verbose --output-format stream-json
& $CLAUDE_EXE plugin install uninstalled-plugin@skope-fixture --scope project
# cwd=other；此次调用后安装元数据新增该 ID 的 user 记录。
& $CLAUDE_EXE --settings "$F/project-only.json" -p /uninstalled-plugin:check --max-turns 1 --verbose --output-format stream-json
```

- allow.json：allowed=true、blocked=false，disableBundledSkills=true。override.json 在此基础上增加 skillOverrides 的 `allowed-plugin:check` 与 `check` 均为 off。
- bundled.json：allowed/blocked=false，disableBundledSkills=false。auto-off.json：三个 marketplace plugin 和两个 auto plugin 全 false，disableBundledSkills=true。project-only.json：同 auto-off，仅 uninstalled-plugin=true。
- allowed/override 的 init.skills 都有 `allowed-plugin:check`，result 都返回唯一 allowed marker；namespace 取 manifest name，ID 仍为目录 basename check；skillOverrides 不能关闭 plugin skill。
- blocked 的 init.skills 无 blocked-plugin:check，result 明确 `Unknown command: /blocked-plugin:check`。auto-off 的 init.plugins 为空，个人自动 plugin 同样 Unknown command。
- bundled=false 对照（disableBundledSkills=true）只剩 doctor；bundled=true 时 init.skills 另有 batch、simplify、verify、debug、loop、claude-api 等。没有执行这些 bundled 技能。doctor 与 init 等内置命令仍存在，开关不承诺移除全部内置命令。
- project-other 成功加载原本仅 project 安装的插件并返回 marker，但 probe 后 installed_plugins.json 新增 user 安装，不能据此宣称其他项目的 cache 安装直接生效。允许列表可能触发 Claude 自身自动安装，skope 只告警 missing 并写 true。

加载根对照：

| 来源/命令 | 证据 |
|---|---|
| directory marketplace | known_marketplaces.json 的 `skope-fixture.source={source:"directory",path:"<F>/market"}`、installLocation=`<F>/market`；catalog 精确 name 对应 source=`./allowed-plugin`。list.installPath 是存在的普通 cache 目录（非 symlink/Junction），init.plugins.path 却是 `<F>/market/allowed-plugin` |
| source-only | 安装后仅源目录新增 skills/live-check/SKILL.md（正文 SOURCE_ONLY_09fc42ab），cache 无该入口。执行 `--settings <F>/allow.json -p /allowed-plugin:live-check --max-turns 1 --verbose --output-format stream-json`，init 列出 live-check 且 result 返回 SOURCE_ONLY_09fc42ab，证明不能扫旧 cache |
| git cache 控制组 | CLI `marketplace add file:///...` 被拒绝；改为仅 fixture 建本地 git 仓库、`git clone <F>/git-market <F>/claude/plugins/marketplaces/skope-git-fixture`，预置 known_marketplaces 的 `source={source:"git",url:"file:///<F>/git-market"}` 与上述 installLocation（extraKnownMarketplaces 同 source）。真实 `plugin marketplace update skope-git-fixture` 退出 0。本实验不声称 CLI add 的 file URL 入口可用 |
| cache-only | 真实 `plugin install cache-plugin@skope-git-fixture --scope user` 从本地 catalog 安装；首次预置错误 installLocation 有刷新告警，修正 clone 到标准位置后 update 成功。`--settings <F>/cache-only.json -p /cache-plugin:check --max-turns 1 --verbose --output-format stream-json` 的 init.path 等于 list.installPath 的 cache，result 返回 CACHE_PLUGIN_09fc42ab |
| cache-scopes-root / cache-native-paths | 仅 fixture installed_plugins.json（version=2, plugins[id]=记录数组）换成 user/project/local 三条、分别指向 1.0.0/1.0.1/1.0.2，各有不同正文 marker；projectPath=repo。记录原序 user 在前时，repo 调用选 1.0.0，返回 CACHE_USER_09fc42ab |
| cache-reverse / cache-reverse-other | 仅反转记录数组为 local/project/user。同样 cache-only 命令在 repo 选 1.0.2 并返回 CACHE_LOCAL_09fc42ab；other 选 1.0.0 并返回 CACHE_USER_09fc42ab，证明按原序首个适用项，不能排序 scope/version |
| cache-reverse-nested | 同命令 cwd=repo/sub，init.plugins.path=1.0.2，说明子目录仍适用 projectPath；随后 provider 返回 403 额度不足，最终模型调用未完成。路径结论来自 init，未声称 marker 成功；当时停止模型调用。Task 17 的后续复验见下方记录 |

cache-only.json 与 auto-off 相同，额外 cache-plugin@skope-git-fixture=true。真实 list fixture 保留原字段并以 `/fixture` 替换临时绝对根；目录来源元数据另存 package-local fixture。仅支持已明确定位的根；未知关键来源或含糊记录 fail-closed，不猜版本，也不运行额外模型 probe。所有控制组以 init 列表、路径及 CLI 确定性拒绝为主，marker 只辅助确认已执行的允许项。

实施决定（独立审查修订）：suppressed 占位是已知清单不完整，不能仅告警后继续。交接后的 Claude 若接受 workspace 信任，可能加载未进入关闭全集的预存自动 plugin；因此精确占位返回类型化 inventory 错误，active dry-run/launch 不进入 Plan、Stage 或 Handoff，提示先在 Claude 独立完成当前 workspace 信任后重试。skope 不修改信任状态，不原样回显 notes；none 仍跳过 inventory。占位 fixture 保留为失败测试输入。此决定基于已完成实验，不需要新的模型调用。

结论：第 10 条门禁通过，spec 已补实际 list 数组、自动 plugin/未信任占位的失败处理、directory 原地加载、cache 记录顺序与 scope 规则；六个冻结类型不变。实验 fixture 和无秘密 harness 暂留供独立复核，未纳入产品源码。

## Codex（阻断 Phase 3）

### 第 1 条：`-c skills.config` 按 `path` 关闭 skill

- 状态：通过（2026-09-06；原生 Linux 实验）。
- agent 版本：Codex CLI 0.153.1；官方 release 的 Linux musl x86_64 二进制 `/var/tmp/skope-p3-codex-01531/codex-x86_64-unknown-linux-musl`，SHA-256 `b9315df68cb0e2827c940ffacb66f7524e820d9744060c755fbf938724ad2b76`。
- fixture：唯一临时 HOME/USERPROFILE/CODEX_HOME、独立 git repo；allow/block、同名 other-id、中文空格目录、两个 symlink 入口共享一个目标。未复制配置、凭据或调用模型。
- 命令：`python3 internal/agent/codex/testdata/verification/path_controls.py /var/tmp/skope-p3-codex-01531/codex-x86_64-unknown-linux-musl`。完整 argv 与 JSON-RPC 协议、fixture 构建见 [实验记录](../internal/agent/codex/testdata/verification/README.md)；脚本启动真实 `app-server --stdio`，经当版 schema 确认的 `skills/list`/`config/read` 观察 path/enabled/来源层，另以真实 `debug prompt-input` 检查模型可见清单。
- 观察：SKILL.md 文件 false 精确关闭；目录 false 无效；同名不同文件互不连带；Unicode/空格文件有效；链接入口文件与 canonical 文件均命中，同目标只返回一条 skill。所有返回 skill 的路径都在 fixture。
- bundled：true 时有 imagegen、openai-docs、plugin-creator、review-agent、skill-creator、skill-installer；false 时全部消失。未创建缓存时 false 不创建 `.system`，true 创建并列出六条。未执行这些 skill。
- 参数：交互/exec 尾部 `-c sandbox_mode="p3-invalid"` 均进入配置校验；独立 `--` 后分别报 `unrecognized subcommand '-c'` / `unexpected argument '-c'`，未应用控制。运行时 `-p fixture` 的配置文件 deny 可被显式 CLI true 覆盖；旧 profile 字段明确报不支持。
- 限制：Windows 同版 CLI 忽略 fixture HOME/USERPROFILE 的 user skill 根，使用系统 Known Folder；初次只读发现真实全局路径后停止，没有把该组计入通过。原生 Windows 路径/Junction 与真实交互模型调用未验，本条通过范围为 Linux 路径规则与模型可见 prompt。

### 第 6 条：项目级 skills 目录的扫描范围

- 状态：未验证
- agent 版本：
- fixture 布局：
- 命令：
- 观察：
- 结论：

### 第 9 条：plugin ID 格式与 `skills.config` 跨层合并语义

- 状态：未验证（路径跨层部分已完成并修订 spec §7.2；plugin ID 等待 Task 2）。
- agent 版本、fixture、完整命令同第 1 条及其实验记录。
- 观察：User path=false、name=false、两者组合的三组规则均不会被 CLI `skills.config=[]` 或仅 block=false 的新数组清除。CLI allow path=true + block path=false 在三组中均恢复 allow；其他同名路径继续禁用。同一路径重复项最后一项胜出。所有 app-server 组的 fixture config.toml 字节不变。
- 交叉验证：`debug prompt-input` 真实输出证明 User path/name 禁用后的 allow 被 CLI true 恢复，block 从模型可见 skill 清单消失；不是仅凭模型自述或退出 0。
- 层边界：trusted project skills.config 出现在 config/read 的有效配置及 project 来源，但 skills/list 不应用该层规则；`-p fixture` 的运行时用户 profile 文件会应用规则且 CLI true 可覆盖。app-server 拒绝 `-p`，因此 profile 结论来自 debug prompt-input。
- 结论：纯 denylist 或空数组不能清除 User 禁用。普通 skill 必须枚举 canonical 路径全集，对选中项显式 true，其余 false，spec §7.2 已修改；不需要产品重定向 CODEX_HOME 或改写用户文件。plugin ID、插件配置层与发现范围仍需 Task 2 验证。

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

### Phase 0

- 日期：2026-09-04
- 环境：Windows 11，Go 1.27.1 windows/amd64
- 状态：通过
- 命令：`make check`
- 观察：`gofmt`、`go vet`、golangci-lint v2.13.2（0 issues）、`go test ./...`、`go build` 全部退出 0
- 命令：`go test -short ./...`
- 观察：全部退出 0；`internal/testutil` 的构建型测试按约定跳过
- 命令：`go test -count=1 -coverprofile=coverage.txt ./...`、`go tool cover -func=coverage.txt`
- 观察：总 statements 覆盖率 48.8%；Phase 0 的 `fakeagent` 端到端测试通过独立子进程执行，不计入当前测试进程覆盖率。80% 是项目目标，当前 `make check` 与 spec §14.7 未把覆盖率设为 Phase 0 门禁
- 命令：从当前 HEAD 创建干净 clone，执行 `go vet ./...`、`go test ./...`、`go build -o skope.exe ./cmd/skope`、`.\skope.exe version`
- 观察：全部退出 0；版本输出 `skope dev`
- 命令：在一次性 clone 中把 `origin` 设为 `https://github.com/scarb/skope.git`，执行 `go run github.com/goreleaser/goreleaser/v2@v2.18.0 check`
- 观察：输出 `1 configuration file(s) validated`。工作仓库的 `origin` 已配置为 `https://github.com/HScarb/skill-scope.git`
- CI：首次 run `33880125575` 暴露 Windows PowerShell 将未引用的 `-coverprofile=coverage.txt` 拆成两个参数；提交 `8604312` 为该参数加引号并升级 `checkout`、`setup-go` 到 v7
- CI：最终 [run 33880710693](https://github.com/HScarb/skill-scope/actions/runs/33880710693) 全部通过：Ubuntu/macOS 执行 `go test -race`，Windows 执行 `go test` 与 build，Ubuntu lint 使用 `golangci-lint-action@v9`；无 Node 20 弃用警告

### Phase 1

- 日期：2026-09-05。
- 状态：实现、本地质量门禁、三平台 CI 和真实 Claude 白名单验证通过。
- 源码基线：`7ff2dff`；真实 Claude 实验二进制基于 `170c857`。两者之间仅修复嵌套 command ID 与相对 TOML 表顺序，均已补回归并通过最新 CI。
- 类型审计：`Skill`、`Location`、`Adapter`、`Capabilities`、`LaunchPlan`、`Inventory` 的字段与本期设计对齐；未因最终修复修改冻结类型。
- 本地环境：Windows，Go 1.27.1；`make check`（含 golangci-lint 0 issues）、`go test -short ./...` 通过。
- 覆盖率：`go test -count=1 '-coverprofile=coverage.txt' ./...` 与 `go tool cover '-func=coverage.txt'`，总 statements **84.4%**。Windows 跳过 Unix exec E2E；Unix 行为由 Linux/macOS CI 执行。
- CI：首次 [run 33940272123](https://github.com/HScarb/skill-scope/actions/runs/33940272123) 暴露 macOS `/var` 实路径、Windows 短路径与 golden CRLF fixture 问题；使用文件身份/canonical path 比较和精确 LF 属性修复。最新源码 [run 33948756305](https://github.com/HScarb/skill-scope/actions/runs/33948756305) 全部通过：Ubuntu/macOS race、Windows test/build、lint。
- 干净 clone：从本地仓库独立 clone 到随机临时目录，更新至 `7ff2dff` 后 `make check` 通过；空 `SKOPE_HOME` 下 `skope claude -s none --dry-run` 退出 0，没有创建配置目录。递归删除被工具策略拒绝，该无认证配置的 clone 保留在系统临时目录 `skope-phase1-clean-f19c86d5630b43a6a5b20a18b77aebd5`。

#### 真实 Claude 对照实验

- Claude Code：**2.1.259**，使用用户现有本地第三方中转配置。仅将必要的 provider/env 值注入实验进程环境，未在仓库、输出或 fixture 中保存凭据。
- 平台：WSL Ubuntu，Linux `6.6.87.2-microsoft-standard-WSL2`；Linux skope 在 ext4 上运行。实际 Claude 是本机 Windows PE，临时 bash bridge 将 skope 生成的 `--settings` 路径经 `wslpath -w` 转换后 exec Claude。此实验验证真实 Claude 配置生效与 skope 会话流程，不宣称使用了原生 Linux Claude；原生 Unix handoff 由 CI 的 fake-agent E2E 证明。
- 隔离目录：`HOME`、`CLAUDE_CONFIG_DIR`、`SKOPE_HOME` 都指向随机 `/tmp/skope-task16.XXXXXX/fixture/` 下的实验目录；没有复用真实 skill、hooks 或 plugin。

```text
fixture/
  home/
  claude/skills/allowed/SKILL.md
  claude/skills/blocked/SKILL.md
  skope/config.toml           # agents.claude.command 指向临时路径桥接
  skope/skillsets.toml        # version=1；phase1.skills=["allowed"]
  repo/
```

两个 `SKILL.md` 的 frontmatter 名与目录名相同，正文分别要求返回 `SKOPE_ALLOWED_MARKER_260905` 和 `SKOPE_BLOCKED_MARKER_260905`。在同一隔离环境下执行以下命令；`bridge` 是临时脚本，未纳入产品代码。

```sh
bridge -p /allowed --max-turns 1 --output-format text
bridge -p /blocked --max-turns 1 --output-format text
skope claude -s phase1 -- -p /allowed --max-turns 1 --output-format text
skope claude -s phase1 -- -p /blocked --max-turns 1 --output-format text
skope claude -s phase1 --dry-run
```

| 检查 | 观察 |
|---|---|
| direct allowed 控制组 | 退出 0，返回 allowed marker |
| direct blocked 控制组 | 退出 0，返回 blocked marker，证明该 skill 原本可被发现及调用 |
| skope allowed | 退出 0，返回 allowed marker |
| 发布配置 | 实读 settings JSON 为 `allowed: "on"`、`blocked: "off"` |
| skope blocked | 未返回 blocked marker；Claude CLI 明确报告由 `skillOverrides` 禁用，见下文 |
| allowed 退出 | 保留 1 个 session |
| blocked 再启动 | 回收前次 session，保留当前 session，数量仍为 1 |
| 最后 dry-run | 退出 0，session 数量变为 0 |

Claude 对 blocked 的确定性拒绝信息为：

```text
Skill "blocked" is disabled via skillOverrides. Remove the override from your settings to run it.
```

该命令退出码为 0，因此验证以明确拒绝信息及 blocked marker 缺失为准，不能仅依赖退出码。所有临时 fixture、bridge、输出和实验二进制已清理。

本次 Phase 1 实验没有验证 projection 或 plugin 枚举，当时 §7.5 第 3、10 条保持「未验证」；后续状态以上方对应条目的独立实验记录为准。

### Phase 2

- 日期：2026-09-06；Phase 2 实现、逐任务 Spec/质量审查、本地与 CI 门禁及真实验收全部通过。Task 17 首次因 provider 403 额度不足暂停；用户更换 provider 后，最终构建的投影资源读取与允许插件调用均复验成功。
- 实现源码：`368f3ffda1cb68c09abd89a13bfb15e64debb2f6`；真实复验及独立 clone：`22fa1f6063dc37611ae14b693f5bc394e45fb460`。后者相对前者仅修改测试，未改变产品行为。
- 冻结审计：对 `27d22be` 检查 `Skill`、`Location`、`Adapter`、`Capabilities`、`LaunchPlan`、`Inventory`，结构不变；`cmd/skope/main.go`、`go.mod`、`go.sum` 无 diff。depguard 通过。
- 本地 Windows Go 1.27.1：`make check` 的 gofmt/vet/lint/test/build 全部通过，golangci-lint v2.13.2 为 0 issues。`go test -count=1 '-coverprofile=coverage.txt' ./...` 及 `go tool cover '-func=coverage.txt'` 通过，总 statements **88.3%**。
- 包覆盖率：Claude 94.9%、CLI 92.1%、launch 95.3%、proc 90.9%、projection 93.7%、skill 95.4%、termsafe 100%。host 67.9%、session 79.7%；部分宿主包装、平台错误分支及子进程执行不计入当前进程覆盖率，未排除业务文件或补无意义 getter 测试抬高数字。
- PR：[#2](https://github.com/HScarb/skill-scope/pull/2)。首轮 [run 34007742994](https://github.com/HScarb/skill-scope/actions/runs/34007742994) 暴露测试辅助进程缺少 GOCOVERDIR、Windows/macOS 路径别名、Windows os.Root symlink 错误差异及 macOS socket 路径过长。`16cc4c9` 与 `22fa1f6` 修复测试，保留安全拒绝、原内容不变、精确流长度和超时断言。
- 第二轮 [run 34008029558](https://github.com/HScarb/skill-scope/actions/runs/34008029558)：Ubuntu/macOS race 与 build、lint 通过；Windows 的只读投影源快照测试失败。诊断提交 `d8b32f6` 的 [run 34008182531](https://github.com/HScarb/skill-scope/actions/runs/34008182531) 显示仅目录 mtime 从旧值变为子链接创建时间。本机 Junction overlay 重复复现后，`60e0456` 将测试的普通目录元数据改为只读句柄 Stat，保留路径、模式、mtime、正文的完整比较；修复后本机连续 50 次通过，WSL 真实 symlink host race 通过。上述 CI 四个 job 的 Go 实际版本均为 1.24.0。
- 最终代码 [run 34008500885](https://github.com/HScarb/skill-scope/actions/runs/34008500885)（`60e0456`）全部通过：Ubuntu/macOS race+build、Windows test+build、lint。此时再次运行 Windows 全仓覆盖率测试通过，仍为 88.3%。相对真实验收产物 `22fa1f6`，只增加上述快照测试诊断及修复，生产文件不变。
- 干净 clone：`git clone --no-local --branch codex/phase2-claude-completion /mnt/d/workspace/vibe/skill-scope /var/tmp/skope-phase2-final-01a070a2/source`，HEAD `22fa1f6`。WSL Go 1.27.1 的 `make check` 全通过；lint 首次缺两个 Linux 专用模块的本地缓存，补下载固定版本后通过，未改 go.mod/go.sum。
- clone 内单独构建的绝对 `source/skope` 与 fakeagent 验证 `claude -s dev --dry-run`：仅一次精确 `plugin list --json`，1 native / 1 projected / 1 unavailable，插件一开一关、bundled 关闭，只列投影文件路径、不含正文、不创建 session。使用最小显式环境，probe 记录与之完全一致，clone 前后均干净；证据在 `/var/tmp/skope-phase2-clean-fixture-01a070a2/`。
- 交付文档提交 `089a5e9` 的 [run 34008624425](https://github.com/HScarb/skill-scope/actions/runs/34008624425) 同样全绿。干净 clone 快进至该提交后，重新 `make check` 及独立 fake-agent dry-run 全通过；后续关闭状态仅修改文档，产品实现不变。

#### 真实 Claude：构建身份与隔离方式

- Claude Code **2.1.259**；实际执行 Windows PE `D:/programs/scoop/apps/nodejs-lts/current/bin/node_modules/@anthropic-ai/claude-code/bin/claude.exe`。skope 在 WSL Ubuntu 的 ext4 运行。此实验验证 Windows Claude 读取设置与 skill 的行为；原生 Unix 进程交接以自动测试及 CI 为证据，Windows 最终 handoff 仍未交付。
- `SKOPE_BUILD_COMMIT=22fa1f6063dc37611ae14b693f5bc394e45fb460`；从上述 Linux clone 构建 `SKOPE_EXE=/var/tmp/skope-phase2-final-01a070a2/skope`。`version` 为 `skope phase2-22fa1f6063dc37611ae14b693f5bc394e45fb460`；build info 为 Go 1.27.1、GOOS=linux、GOARCH=amd64、CGO_ENABLED=1、vcs.revision 同提交、vcs.modified=false。
- 该产物 SHA-256：`aac65bf337763c3e48ede652ec168248ae8b63f0cd532666520534f8cfdbc9bc`。
- 首次直接从 Windows worktree 进行 WSL 构建时，VCS stamp 误取主工作区 `27d22be`。该产物曾成功返回原生、投影资源和允许插件 marker，但最终验收不沿用此身份有歧义的产物；改用干净 clone 并先核对 build info 再复验。
- Windows fixture 为 `C:/Users/j00466872/AppData/Local/Temp/skope-phase2-final-01a070a2`；下文 `$FIXTURE` 为其 `/mnt/c/...` 映射。临时 `bridge.py` 调用 PowerShell bridge，后者直接创建 Claude PE 子进程。probe 的 installPath/projectPath 转为 Linux 可读路径；launch 的 settings/add-dir 转为 Windows UNC 路径。桥接固定 fixture cwd，显式 UTF-8、stdin EOF、60 秒上限；路径转换只存在于实验脚本。
- bridge 仅在进程内读取现有 provider 所需 env 值，注入实验 Claude 并替换输出中的具体凭据/URL；未保存或修改真实 settings/skills/hooks/plugins。模型命令使用较短 system prompt、Read 工具和 low effort 减少上下文，不禁用 skills/plugins，不宣称测试了默认完整 system prompt。

```text
Windows fixture/
  home/.codex/skills/
  home/.agents/skills/projected-check/
    SKILL.md
    references/marker.md
  claude/skills/{native-check,blocked-check}/SKILL.md
  claude/skills/{allowed-plugin,blocked-plugin}/
    .claude-plugin/plugin.json
    skills/check/SKILL.md
  repo/.git/
/var/tmp/skope-phase2-final-01a070a2/
  source/                    # 干净 clone
  skope                      # 上述绑定版本的产物
  skope-home/config.toml
  skope-home/skillsets.toml
  skope-home/sessions/        # ext4，最后 dry-run 后为空
```

两个原生 skill 的 frontmatter name 等于目录名，正文要求输出 `SKOPE_NATIVE_CHECK_01a070a2` 或 `SKOPE_BLOCKED_CHECK_01a070a2`。两个自动 plugin 的 manifest 为 `{"name":"<目录名>","version":"1.0.0"}`；skill name=check，正文分别要求输出 `SKOPE_ALLOWED_PLUGIN_01a070a2` 或 `SKOPE_BLOCKED_PLUGIN_01a070a2`。投影 skill 使用上方第 3 条同样的不同 frontmatter 名和读取相对资源的指令；资源唯一内容为 `SKOPE_PROJECTED_RESOURCE_01a070a2` 加换行。

config.toml 的 `agents.claude.command` 为 `/mnt/c/Users/j00466872/AppData/Local/Temp/skope-phase2-final-bridge.py`。skillsets.toml 完整内容：

```toml
version = 1
[skillsets.phase2]
skills = ["native-check", "projected-check"]
bundled = false
[skillsets.phase2.plugins]
claude = ["allowed-plugin@skills-dir"]
[skillsets.with-bundled]
skills = ["native-check"]
bundled = true
```

构建与复验命令（`go` 指本次 WSL Go 1.27.1；实际由临时 Python runner 顺序执行并分别收集输出）：

```sh
cd /var/tmp/skope-phase2-final-01a070a2/source
SKOPE_BUILD_COMMIT=22fa1f6063dc37611ae14b693f5bc394e45fb460
SKOPE_EXE=/var/tmp/skope-phase2-final-01a070a2/skope
go build -ldflags "-X main.version=phase2-$SKOPE_BUILD_COMMIT" -o "$SKOPE_EXE" ./cmd/skope
"$SKOPE_EXE" version
go version -m "$SKOPE_EXE"
sha256sum "$SKOPE_EXE"
FIXTURE=/mnt/c/Users/j00466872/AppData/Local/Temp/skope-phase2-final-01a070a2
export HOME="$FIXTURE/home" USERPROFILE="$FIXTURE/home"
export CLAUDE_CONFIG_DIR="$FIXTURE/claude" CODEX_HOME="$FIXTURE/home/.codex"
export SKOPE_HOME=/var/tmp/skope-phase2-final-01a070a2/skope-home
cd "$FIXTURE/repo"
for prompt in /native-check /projected-check /blocked-check /allowed-plugin:check /blocked-plugin:check; do
  "$SKOPE_EXE" claude -s phase2 -- -p "$prompt" --max-turns 2 --verbose --output-format stream-json --system-prompt 'Follow the invoked skill instructions exactly.' --tools Read --allowedTools Read --effort low
done
"$SKOPE_EXE" claude -s with-bundled -- -p /blocked-check --max-turns 2 --verbose --output-format stream-json --system-prompt 'Follow the invoked skill instructions exactly.' --tools Read --allowedTools Read --effort low
"$SKOPE_EXE" claude -s phase2 --dry-run
```

| 检查 | `22fa1f6` 产物的观察 |
|---|---|
| 原生允许项 | 退出 0，返回 `SKOPE_NATIVE_CHECK_01a070a2` |
| 白名单初始化 | init.skills 为 native-check、projected-check、allowed-plugin:check、doctor；init.plugins 仅 allowed-plugin，path 为 fixture 的原地目录 |
| 原生禁止项 | 退出 0，明确报告 `Skill "blocked-check" is disabled via skillOverrides...`；未返回 blocked marker |
| 插件禁止项 | 退出 0，明确报告 `Unknown command: /blocked-plugin:check`；未返回 blocked marker |
| bundled 开关 | false 时可选 bundled skills 消失、doctor 保留；true 时出现 verify、debug、simplify、batch、loop 等；未调用这些 bundled skills |
| 投影资源模型调用 | 更换 provider 后退出 0，Read 的 tool_use 路径指向 `sessions/claude-20260906-112355-7f71/claude/addDir/.claude/skills/projected-check/references/marker.md` 的 Windows UNC 映射；对应 tool_result 及最终 result 均含 `SKOPE_PROJECTED_RESOURCE_01a070a2`，is_error=false |
| 允许插件模型调用 | 同一产物退出 0，最终 result 为 `SKOPE_ALLOWED_PLUGIN_01a070a2`，is_error=false；init.plugins 仅包含 fixture 中的 allowed-plugin |
| session 生命周期 | 每次退出保留当前 session，下一次启动回收前次；最终 dry-run 退出 0，sessions 数量由 1 变 0 |
| dry-run 配置 | blocked-check=off、native-check/projected-check=on；allowed-plugin=true、blocked-plugin=false；disableBundledSkills=true；只显示投影路径，不显示正文 |

最终补验继续使用上述完整命令、Claude 2.1.259、隔离 fixture 和绑定 `22fa1f6` 的产物，运行前 version 与 SHA-256 均与记录一致。bridge 每次读取当前 provider 所需 env，未复制用户配置；两次调用的 stderr 含 Claude 的 `unrecognized_model` 诊断，均正常完成且 is_error=false。该诊断不作为 skill 生效依据，结论来自实际 Read 工具结果和准确 marker。最后再次 dry-run 退出 0，回收剩余 session 至 0；Task 17 Step 4 关闭。

首次额度不足及桥接编码问题保留为实验历史，不再是待办。补验后检查 Windows fixture 与 skope-home 的 33 个文件，未发现当前 provider 的 AUTH_TOKEN/API_KEY 值。实验脚本、fixture 和脱敏输出暂留供交付复核，不纳入产品提交。
