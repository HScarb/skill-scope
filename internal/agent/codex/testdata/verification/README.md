# Codex Phase 3 真实验证

2026-09-06，原生 Linux Codex CLI **0.153.1**；WSL Ubuntu，x86_64。二进制由官方 release `rust-v0.153.1/codex-x86_64-unknown-linux-musl.tar.gz` 解压到 `/var/tmp/skope-p3-codex-01531/`，未安装到全局 PATH。可执行文件 SHA-256 为 `b9315df68cb0e2827c940ffacb66f7524e820d9744060c755fbf938724ad2b76`。

```sh
python3 internal/agent/codex/testdata/verification/path_controls.py /var/tmp/skope-p3-codex-01531/codex-x86_64-unknown-linux-musl
```

脚本只用 Python 标准库，在 /var/tmp 创建唯一临时目录，避免 WSL 重启清理 /tmp。先核对真实 `--help`、`exec --help`、`app-server --help`、`generate-json-schema --help`，再生成该二进制的协议 schema。子进程环境只继承 PATH 和系统执行所需变量，HOME、USERPROFILE、CODEX_HOME 指向 fixture；不传认证变量，不读取或复制 auth.json，不调用模型。最后一次完整矩阵的原始结果留在 `/var/tmp/skope-p3-controls-0mokvdra/`，不提交日志、缓存或系统 skill 正文。

```text
fixture/
  home/.agents/skills/{allow,block,other-id,中文 space}/SKILL.md
  home/.agents/skills/{link-one,link-two} -> fixture/target
  target/SKILL.md
  codex/config.toml
  codex/fixture.config.toml
  codex/skills/.system/             # Codex 自动创建
  repo/.git/
  repo/.codex/config.toml          # 仅项目对照存在
  schema/
  results.json                     # 每组完整 argv、fixture 配置、协议结果
  parser-results.json
  prompt-results.json
```

allow 与 other-id 的 frontmatter 同为 p3-allow；block 为 p3-block；链接目标为 p3-link。每个正文仅有唯一 PHASE3 marker。路径经 JSON 基本字符串编码生成 TOML 基本字符串，覆盖反斜杠、中文与空格，不经 shell 拼接参数。

每组启动 `[EXE, "app-server", "--stdio", "-c", OVERRIDE, ...]`，依次发送 initialize（clientInfo name=skope_fixture/version=1）、initialized 通知、`skills/list`（cwds=[fixture/repo]、forceReload=true）、`config/read`（cwd=fixture/repo、includeLayers=true）。读取完关闭实验子进程。schema 确认 skills/list 返回 path、enabled、scope；disabled skill 仍在列表，但 enabled=false。

| 对照 | 真实观察 |
|---|---|
| 无规则 → block 文件 false | allow/block 均 true → 仅 block false |
| 空白名单 | allow/block 文件均 false；bundled 另行控制 |
| block 目录 false | 无效，block 仍 true |
| 中文空格文件 false | 精确关闭 |
| 同名不同目录，仅 allow false | allow false，other-id true |
| 两个链接入口同目标 | 只列一条 canonical target/SKILL.md |
| 链接入口 SKILL.md false / canonical 文件 false | 两者均关闭同一 target |
| User path=false / name=false / 两者组合 | 对应路径或所有同名路径禁用 |
| 三种 User 规则 + CLI [] | 原禁用仍在 |
| 三种 User 规则 + CLI 仅 block=false | 原禁用仍在，block 也 false |
| 三种 User 规则 + CLI allow=true/block=false | allow true、block false；原 name 规则继续关闭 other-id |
| 重复路径 false→true / true→false | 最后一项胜出 |
| trusted project 文件中 allow=false | config/read 确认进入项目层，但 skills/list 中 allow 仍 true |
| project deny + CLI [] / true | 前者 allow 仍 true；后者 allow true/block false |
| bundled true/false，缓存已存在 | 六条 system skills 出现/消失 |
| 缓存未创建 + bundled false/true | false 不创建 .system；true 创建并列出六条 |

六条 system skills 为 imagegen、openai-docs、plugin-creator、review-agent、skill-creator、skill-installer。所有 33 组 app-server 实验中 config.toml 字节保持不变；profile-v2 的两组被 app-server 明确拒绝，不算控制通过。

额外调用真实 `debug prompt-input`（先核对帮助），直接检查模型可见 prompt：baseline 含 allow/block；block=false 后 blocked 条目消失；User path/name deny 经 CLI 精确 true 后 allow 恢复、block 消失。`-p fixture` 在运行时读取 fixture.config.toml，allow deny 生效，CLI true 可恢复。此证据验证 prompt 加载结果，没有让模型自述或执行 skill。旧 `profile="fixture"` 配置被明确拒绝，提示改用 `--profile fixture` 与 `fixture.config.toml`。

完整参数形状如下；ALLOW/BLOCK 是上述 fixture 文件的经 TOML 编码的绝对路径，逐组展开 argv 保存在 prompt-results.json。User path/name 对照先写入表中的配置，再运行第三行；profile 文件含 allow path=false。

```text
EXE debug prompt-input "fixture prompt"
EXE debug prompt-input "fixture prompt" -c 'skills.config=[{path=BLOCK,enabled=false}]'
EXE debug prompt-input "fixture prompt" -c 'skills.config=[{path=ALLOW,enabled=true},{path=BLOCK,enabled=false}]'
EXE -p fixture debug prompt-input "fixture prompt"
EXE -p fixture debug prompt-input "fixture prompt" -c 'skills.config=[{path=ALLOW,enabled=true},{path=BLOCK,enabled=false}]'
```

交互/exec 的尾部 `-c sandbox_mode="p3-invalid"` 均触发配置枚举值错误，证明参数确实进入配置解析；独立 `--` 后分别报 unrecognized subcommand '-c' / unexpected argument '-c'。这证明尾部控制不生效，未把解析器错误当作 skill 调用证据。真实交互/exec 模型调用不在本实验范围。

最初固定的 Windows 二进制为 `C:/Users/j00466872/AppData/Local/OpenAI/Codex/bin/1e3e57cdf0634c02/codex.exe`，版本同为 0.153.1。它在重定向 HOME/USERPROFILE 后仍用系统 Known Folder 枚举真实 .agents/skills；该只读意外发现未用于门禁，随后停止 Windows 实验。脚本因此拒绝 Windows 执行，不能把过滤输出声称为来源隔离。Linux 对照没有 fixture 外 skill；Unix /etc/codex/config.toml 的实际层为空。

结论：路径控制成立；原「用 CLI 新数组清除 User deny」假设不成立，须显式输出普通路径全集的 true/false，spec §7.2 已按证据修订。plugin ID 与来源发现交给 Task 2，不能据此关闭完整第 9 条门禁。

## Task 2：发现范围与本地 plugin 元数据

版本、SHA-256 与上文相同。最终矩阵使用独立 user、mount、network namespace；`/etc` 先挂载私有 tmpfs，退出自动消失。未写宿主 `/etc/codex`；子进程不具备外部网络、认证、MCP 或 hooks，未调用模型。

```sh
unshare --user --map-root-user --mount --net python3 internal/agent/codex/testdata/verification/discovery_plugins.py /var/tmp/skope-p3-codex-01531/codex-x86_64-unknown-linux-musl --private-admin
```

脚本保存完整 argv、配置、skills/list（含 enabled/pluginId）、config/read（含 layers）、明确请求的 plugin/list/install/installed/uninstall 返回值。53 组 app-server 对照、3 组 debug prompt-input、features 帮助/默认/CLI false 三组命令均完成；核心结果有显式断言。原始证据在 `/var/tmp/skope-p3-catalog-3a_qkqmt`；每次重跑会创建另一个唯一目录。只提交 harness 和相邻 `catalog/` 最小 fixture，不提交数据库、系统 skill 正文或完整日志。

审查后补强非空路径断言与 RPC 顶层 error 检查，同一 53+3+3 矩阵完整复验通过，结果在 `/var/tmp/skope-p3-catalog-e7vs43pt`。无效 frontmatter 的 `result.data[].errors` 仍作为预期业务结果保留。

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
  home/.agents/plugins/marketplace.json
  home/plugins/{alpha,beta,candidate}/.codex-plugin/plugin.json
  codex/plugins/cache/fixture/{alpha,beta}/<version>/
  codex/{config.toml,fixture.config.toml}
  repo/.codex/config.toml
  results.json  prompts.json  features.json  assertions-passed.txt
private-namespace:/etc/codex/{config.toml,skills/admin/SKILL.md}
```

每个 skills 根还包含 group/nested、.hidden、container/SKILL.md 与 container/child/SKILL.md、两个同 name 不同 basename、skill symlink 与中间目录 symlink。SKILL.md 只有唯一测试名、description 和 marker。自动命名空间实验在每个根放置 auto-<root>/.codex-plugin/plugin.json，manifest.name 与 basename 不同，并同时放根 SKILL.md 与 skills/one/SKILL.md。

| 对照 | 真实结果 |
|---|---|
| repo 根 cwd | 全局两个根、repo 的 .agents/skills 与 .codex/skills |
| apps/api cwd | 再加 apps 与 apps/api 的两个兼容根；没有 sibling/descendant |
| 无 .git 的 apps/api cwd | 仅 cwd 两个根与全局根；不扫祖先 |
| skills 根内部 | 递归；已有 SKILL.md 的目录仍继续扫描子目录；隐藏目录不加载；链接文件目标 canonical 去重 |
| symlink 指回 skills 根 | 请求完成且无重复；不据此推测内部深度/visited 实现 |
| frontmatter 缺 name / 缺 description 或无 frontmatter | 缺name用目录basename；后两种返回errors且不加载 |
| /etc/codex/skills | scope=admin；仅在私有 tmpfs 实验 |
| 本地 install alpha/beta | ID=alpha@fixture/beta@fixture；写 config.toml enabled=true 与 cache，无独立安装索引 |
| plugin/list 候选 / config-only / cache-only | 候选非安装；无 cache 的配置不加载；删除配置保留 cache 不加载且 installed 不列 |
| disabled | installed=true、enabled=false，plugin skills 不列 |
| 默认 / 显式 skills 根 | skills/ 与 manifest skills="./custom" 均加载；同插件两条 skill 均出现 |
| 原地 source 修改 / source manifest 缺失 | 已安装技能仍取 cache；installed.localVersion 却跟随 source，所以不能用其选择 active |
| 重装 v2 / 人工保留 v1 | 重装清除 v1；恢复 v1 后 v2 active；移走 v2 后 v1 active |
| 1/2/9/10/aaa / future mtime | aaa active，改10的mtime仍aaa；移走aaa后10 active |
| 空 zzz / local | zzz 令alpha全部技能消失，不回退；local 存在则优先 |
| prerelease / build | alpha.10 胜 alpha.2；release 胜 prerelease；3.0.0+10 胜 +2 与3.0.0 |
| marketplace 缺失 | 已配置 cache 继续加载 |
| uninstall 后恢复旧 cache | 无配置键，不自动加载；缓存残留不能直接视作当前安装 |
| CLI plugins."alpha@fixture".enabled=false | 无效，config/read 保留含字面引号的另一个键 |
| CLI plugins.alpha@fixture.enabled=false / 整体 plugins={...} | 有效；整体 TOML 表可准确编码含点号的 ID，不拆 dotted key |
| User disabled + CLI true | 同插件全部 skill 恢复 |
| plugin skill path=false / namespace name=false | 可单独禁用一条；无 namespace 的同名规则不命中；CLI精确 path=true 恢复 |
| 普通根里有 plugin manifest | namespace 用 manifest.name；根与子层SKILL都加载，pluginId=null，installed不列；dev@skills-dir=false 无效，逐canonical path=false 有效 |
| project | untrusted层忽略；trusted层关闭alpha，CLI true恢复；不同层的仅配置ID仍在config/read |
| runtime profile | -p fixture 从 fixture.config.toml 关闭alpha，debug prompt-input无alpha；CLI true恢复 |
| system config | system层alpha=false生效，User与CLI均能覆盖；admin-only ID进入有效plugins表 |
| bundled true + User单项deny | imagegen按name/path仍false；true只允许bundled来源，不清除单项deny |
| remote_plugin feature | features list 默认 stable true，CLI features.remote_plugin=false后false；本地alpha/beta仍可加载 |

活动版本算法另由固定 tag [store.rs:158–177、737–740](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core-plugins/src/store.rs) 对照：有效版本目录、local优先、双方semver时Rust Version比较，否则字符串比较。上述每项排序有真实矩阵；混合版本比较不必构成全序，产品对不能确定唯一最高项的异常集合应停止，不猜Rust sort结果。

本地可扫描并集为当前cwd至git根的两类skills根、HOME/.agents/skills、CODEX_HOME/skills、Unix admin根，以及相关配置层plugins键对应cache活动根；普通manifest命名空间入口留在普通路径集合。配置层并集须包含User、system、祖先项目文件和运行时profile，不能只读User。允许plugin的全部skill也须显式path=true以覆盖User单项deny；禁止plugin同时写总开关false。原计划的引用点分plugin键和排除plugin路径的做法已修订spec。

支持边界：固定 tag [features/src/lib.rs:1368](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/features/src/lib.rs)、[manager.rs:647](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core-plugins/src/manager.rs) 与 [loader.rs:230–309](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core-plugins/src/loader.rs) 显示 remote_plugin默认true，Codex backend认证时可取远端配置替换本地enabled。此实验无认证，只证明feature能设false与本地开关有效，**没有验证认证远端缓存的关闭语义**。Task3落实活动隔离显式remote_plugin=false与本地来源限定，none透传；不能把本条本地通过当作所有Codex来源均可隔离。

active参数边界交给Task3：拒绝改变cwd/profile/CODEX_HOME、plugins与skills父表/根、marketplace来源、远端feature的用户覆盖，或先纳入完整可验证清单；不要静默接受未知来源扩展。features父表与--enable/--disable远端feature的绕过形式亦须检查；其语法和冲突实现由后续任务验证。Windows Known Folder、远端目录/认证安装、额外agent-plugin manifest格式仍未覆盖。bundled=true保留用户单项deny；缺失允许plugin只写true并报告missing，不保证未来安装后覆盖现存name deny。


## Task 9：CLI 参数形式与 key 空白（2026-09-06）

固定 Linux 0.153.1，新增可复验脚本只使用独立 user/mount/network namespace，映射 root 后将 `/etc` 挂载为临时 tmpfs；HOME、USERPROFILE、CODEX_HOME、cwd 均指向新 fixture，无认证变量，无模型调用。脚本拒绝未映射的 user namespace。调用方式：

```sh
unshare --user --map-root-user --mount --net python3 internal/agent/codex/testdata/verification/cli_forms.py /var/tmp/skope-p3-codex-01531/codex-x86_64-unknown-linux-musl
```

- 20 组 `features list` 全部断言退出码和 remote_plugin 的预期布尔值。`-c value`、`-c=value`、`-cvalue`、`--config value`、`--config=value` 均生效。
- 仅整个 key 的首尾空白被去除。` features.remote_plugin = false` 生效；`features .remote_plugin=false`、`features. remote_plugin=false` 不生效。dot 分段内部空白不会被 trim，这纠正了 spec §6.2 和 Task 9 原来的逐段 trim 假设。
- `"features".remote_plugin`、`features."remote_plugin"` 均未控制真实 remote_plugin；CLI 引号是字面字符，不解析 TOML quoted keys。checker 仍保守拒绝字面 skills/plugins 等父表及其子树。
- `--enable/--disable remote_plugin` 与 `=remote_plugin` 均生效；`--enable remote_plugin` 无论位于 `-c features.remote_plugin=false` 前后都令最终值为 true，不能依靠最后追加 `-c` 修复。
- 真实 `--help` 列出 `-C/--cd`、`-p/--profile`。额外 10 组 `debug prompt-input` 覆盖两种选项的短名分离、短名等号、短名附着、长名分离、长名等号，每组断言退出 0 且输出含 fixture prompt。这里验证参数形式可被运行时接受；profile 加载效果已有上面的独立运行时对照。`features list` 不支持 profile，不能以其拒绝为选项不支持的证据。
- 最新完整结果与 source 参数的 stdout/stderr 在 `/var/tmp/skope-p3-cli-forms-c37f1tgm/`。提交脚本保留全部 case 与断言，临时目录不是唯一复验依据。不提交完整 prompt、缓存或系统 skill 正文；未扩大为 Windows Known Folder/交互模型验证。


## Task 13：真实 skope 启动链验收

`skope_acceptance.py` 使用最终干净 clone 构建的 skope 绝对路径，并强制校验上文 Codex 0.153.1 SHA-256。脚本检查映射 root 的 user namespace 和仅含 loopback 的网络环境，先 make-rprivate 再挂载私有 /etc tmpfs；挂载失败即停止。全部 HOME、USERPROFILE、CODEX_HOME、SKOPE_HOME、CLAUDE_CONFIG_DIR、repo 与源文件在新的 /var/tmp fixture，无认证变量、外网或模型调用。

```sh
unshare --user --map-root-user --mount --net python3 internal/agent/codex/testdata/verification/skope_acceptance.py "$SKOPE_EXE" /var/tmp/skope-p3-codex-01531/codex-x86_64-unknown-linux-musl
```

23 组 skope 命令覆盖 path/name/组合 User deny 的 none→active→dry-run 对照、bundled、空白名单、普通 manifest 根自身/子 skill、remote feature 和空 inventory。真实 `debug prompt-input` 输出的根别名先展开，再对实际 skill 条目做 canonical path 比较；同名路径不会混淆，symlink 的展示入口与实际目标也不会被误当成两个文件。bundled=true 与同 User 配置的 none 可见 system 集合精确一致，imagegen 单项 deny 保留，openai-docs 明确存在。磁盘上的 review-agent 不出现在这版 prompt 中，不把磁盘存在等同模型可见。

每次 prompt/features 命令在原 skope PID 的 /proc/exe、/proc/cmdline 观测真实 Codex 接管，保存完整 delegate argv，并核对 active 最后的 remote_plugin=false。session owner 的 PID 必须相同；session 仅含 owner.json，后续 dry-run/none 回收。config、安装 manifest、源 SKILL/command 文件在启动前固定旧 mtime，每条命令后核对字节及 mtime 完全一致；Codex 自身 fixture cache/session 写入不计作 skope 配置改写。

复跑会打印唯一目录，保存 results.json（含完整 skope/delegate argv、prompt、owner）和 summary.json；这些原始输出与 bundled 正文不提交仓库。最终构建身份、SHA-256、目录和 CI 链接见仓库 docs/verification.md 的 Phase 3 记录。这里证明真实 Codex 的 prompt 加载与 Unix handoff，不宣称模型执行 skill 或认证远端插件已验证。

最终代码审查补充 CODEX_HOME 反例：尾空格目录必须按原值扫描；纯空白是相对路径，active 报错；含符号链接和 `..` 的根在 active dry-run/launch 前拒绝，none 仍按真实 CLI 语义加载。修复前 `13a7ba4` 产物在新增 space-home-active 断言失败，未允许 skill 确实进入 prompt；修复后完整矩阵须重新执行，最终身份见 docs/verification.md。
