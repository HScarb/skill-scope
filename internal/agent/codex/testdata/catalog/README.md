# Codex 0.153.1 本地 plugin 最小事实

这些文件来自 `verification/discovery_plugins.py` 构造并经真实 CLI 安装/加载的无网络 fixture，保留真实字段形状，不是完整用户配置。没有独立 installed_plugins.json 安装索引，不能创建假索引来迎合旧计划。

- `config.toml` 放到 `FIXTURE_CODEX/config.toml`；`cache/` 放到 `FIXTURE_CODEX/plugins/cache/`。
- `marketplace.json` 放到 `FIXTURE_HOME/.agents/plugins/marketplace.json`。其中 `./plugins/alpha` 实际解析为 `FIXTURE_HOME/plugins/alpha`，不是 marketplace.json 所在目录下。源 plugin 需另复制到该路径后才能安装。
- `FIXTURE_CODEX`、`FIXTURE_HOME` 是文档替换 token，代表隔离绝对目录；提交的 JSON/TOML 不含真实用户绝对路径、安装时间或认证。
- alpha 2.0.0 是活动版本，1.0.0 为人为保留的旧缓存。真实重装会删除旧版本。beta 已安装但配置关闭，custom 是 manifest 显式 skill 根；missing 仅配置、没有缓存。
- 本地 ID 为 `alpha@fixture`。manifest 是 `.codex-plugin/plugin.json`；未配置 skills 时读 `skills/`，字符串 `"./custom"` 实测有效。安装后的模型可见名为 `alpha:p3-alpha-v2`。
- cache 活动目录由版本子目录决定。`local` 优先；两段都是合法 semver 时按 semver 比，否则按字符串比。空的最高目录也会被选中，缺失 manifest 不回退旧版本。mtime、源 manifest version、plugin/installed.localVersion 都不能替代这个选择。
- 配置键删除而保留 cache，skills/list 不加载；disabled 配置与有效 cache 仍由 plugin/installed 报 installed=true。marketplace 候选、仅配置键不等于有效本地安装。marketplace 消失不妨碍已配置 cache 加载。
- 普通 skills 根中的 `.codex-plugin/plugin.json` 只提供 manifest.name 命名空间；这些 skill 的 pluginId=null，plugin/installed 不列它们，`plugins.dev@skills-dir.enabled=false` 无效。必须按普通路径控制。

版本选择的辅助依据是固定 tag 的 [PluginStore::active_plugin_version 与 compare_plugin_versions](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core-plugins/src/store.rs)。真实矩阵覆盖 local、1/2/9/10、aaa、空 zzz 及未来 mtime，详见相邻 verification README。

本 fixture 不证明远端安装状态、认证目录服务或 `.codex-remote-plugin-install.json` 的契约。固定 tag [loader.rs](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core-plugins/src/loader.rs) 的 remote 配置分支可替换本地 enabled；实现前须明确拒绝未验证远端来源或补独立控制方案，不得把本地通过扩展为远端通过。
