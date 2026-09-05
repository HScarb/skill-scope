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

本实验没有验证 projection 或 plugin 枚举，§7.5 第 3、10 条仍保持「未验证」，继续阻断 Phase 2 的对应能力。
