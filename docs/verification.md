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
