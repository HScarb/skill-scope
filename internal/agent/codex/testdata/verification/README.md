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
