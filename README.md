# QuailEgg

QuailEgg 是一个自动发版工具。代码合并到主分支后，QuailEgg 汇总 `.changes/` 中的变更记录，计算版本号，更新 `CHANGELOG.md`，然后发布软件包并创建 Git Tag 与 GitHub Release。

## 特性

- 每次改动必须附带 `.changes/` 变更记录，由 git hook 强制检查，生效目录可配置
- pre-commit hook 检查完记录后执行 `[verify].commands`；校验失败即拒绝提交，可用 `git commit --no-verify` 跳过
- 根据模板汇总 `.changes/`，生成 `CHANGELOG.md`
- 按 SemVer 规则计算版本号，发布方式可配置为「发布 PR」或「直接提交」
- 自动识别项目类型，支持 npm、PyPI、crates.io 与 Go module（Go 的版本就是 git tag）
- 发布步骤记录状态，失败后可从中断处恢复，不会重复发布
- `qegg init` 同时生成 GitHub Actions 工作流，合并到主分支即自动发版

## 使用

```bash
qegg init        # 生成配置、git hook 和 GitHub Actions 工作流
qegg commit      # 生成变更记录并提交
qegg check       # 检查环境与配置
qegg plan        # 预览发版计划，不修改任何文件
qegg verify      # 执行发版前校验命令
qegg changelog   # 根据 .changes/ 生成 CHANGELOG.md
qegg release     # 执行发布
qegg resume      # 恢复中断的发布
qegg status      # 查看发布状态
qegg lang zh     # 设置界面语言，对所有项目生效（默认英文）
```

## 发布方式

`[release].mode` 决定版本号如何落地，也决定有没有人工评审：

- `pr`（默认）：合并到主分支后，qegg 把版本提升、CHANGELOG 更新和记录删除放进一个发布 PR；PR 合并后再发布并创建 GitHub Release。
- `commit`：qegg 直接把版本提升提交到主分支，随后发布、打 Tag 并创建 GitHub Release。

两种模式都遵循“先提升版本，再触发发布”。`.quailegg/release.json` 记录待发布的版本；流程中断后重跑 `qegg release` 或 `qegg resume` 会从断点继续，已经发布过的版本不会重复发布。

发版前会执行 `[verify].commands` 中的命令（测试、lint、构建），任何一条失败都会在写入文件之前中止。

`qegg init` 生成三个工作流：`qegg-release.yml`（合并后发版）、`qegg-pr.yml`（PR 校验与版本预览评论）、`qegg-status.yml`（手动查看状态）。

## 语言

界面文案默认英文，支持英文（`en`）与中文（`zh`）。文案集中在 `internal/i18n/locales/en.toml` 与 `zh.toml`，代码里不允许散落用户可见文本。

```bash
qegg lang            # 查看当前语言与可选语言
qegg lang zh         # 写入用户级配置，对所有项目生效
qegg --lang zh plan  # 仅本次覆盖
QEGG_LANG=zh qegg plan
```

优先级：`--lang` > `QEGG_LANG` > 用户级配置 > `en`。语言属于用户，不随仓库走；旧版 `qegg init` 写进 `.quailegg.toml` 的 `language` 键会被忽略（保留该键只为让旧配置继续加载，可以删除）。

用户级配置放在系统的配置目录下：

| 平台 | 路径 |
| --- | --- |
| Windows | `%APPDATA%\qegg\config.toml` |
| macOS | `~/Library/Application Support/qegg/config.toml` |
| Linux | `$XDG_CONFIG_HOME/qegg/config.toml`（默认 `~/.config/qegg/config.toml`） |

## 变更记录

每次修改都需要在 `.changes/` 下新增一个 Markdown 文件，一个文件对应一条记录。文件名用一到两个单词概括本次修改内容，例如 `.changes/cache-expiry.md`：

```markdown
---
type: feat
scope: cache
packages:
  cache: minor
  server: patch
---

新增缓存过期策略配置，支持自定义缓存有效期。
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | 是 | `feat`、`fix`、`perf`、`docs`、`refactor` 等，决定 Changelog 分组 |
| `scope` | 否 | 影响范围，如 `cache` |
| `packages` | 是 | 参与本次改动的包，以及各自的升级类型 `patch`、`minor`、`major` |
| `release` | 否 | 是否参与发版，默认 `true`；`false` 时只记入 Changelog |

`packages` 写成映射，一个包一行，对应 TUI 里为每个包选的升级类型。单包项目只写一项，例如 `cache: minor`。升级类型为 `major` 即表示不兼容变更，不再单设 `breaking` 字段。


`qegg commit` 会生成变更记录并提交，省去手写元数据。无论带不带 `-m` 都会进入交互界面：

```bash
qegg commit
qegg commit -m "feat(cache): 支持自定义缓存有效期"
```

交互流程：

1. 确认变更说明（提交信息），带 `-m` 时预填
2. 填写变更说明（写入 CHANGELOG）；默认取提交信息的描述部分，可以改成更详细的说明，也可以留空
3. 选择需要升版本的包，依赖了改动包的其他包会被自动列出并默认勾选
4. 为每个包选择升级类型（patch / minor / major），例如“支持自定义缓存有效期”
5. 确认后生成记录并提交

例如只改了包 1，包 2 依赖包 1，界面会同时勾选包 1 和包 2，由你决定包 2 是否跟着升版本。

```toml
[changes]
dir = ".changes"
# hook 生效的目录；init 默认写入检测到的包目录，留空表示全仓库
scope = ["src", "crates"]
```

`qegg init` 生成新配置时，`scope` 默认取自检测到的包目录（例如 `packages/core`、`packages/cli`），仓库根部的配置、文档和工作流因此不会触发 hook。由 qegg 自己写出的文件（`.quailegg.toml`、`.changes/README.md`、`.github/workflows/qegg-*.yml`）始终不要求变更记录，`qegg release` 的版本提交也会跳过该 hook。

## 配置

在项目根目录创建 `.quailegg.toml`：

```toml
schema_version = 1

[project]
default_branch = "main"

[git]
tag_prefix = "v"
remote = "origin"

[release]
mode = "pr"                 # pr | commit
github_release = true
commit_message = "chore(release): v{{version}}"

[changes]
dir = ".changes"
# hook 生效的目录；init 默认写入检测到的包目录，留空表示全仓库
scope = ["src", "crates"]

[verify]
commands = ["npm test"]     # 发版前与 pre-commit hook 都会执行
timeout = "15m"

[publish]
registries = ["npm"]

[packages.my-package]
path = "."
resolver = "npm"            # npm | python | rust | go
publish = true
```

未填写的配置项使用自动检测结果或默认值。发布凭证通过环境变量或 CI Secrets 提供，不要写入配置文件。`qegg init` 生成的 release 工作流只注入项目实际使用的生态对应的凭证：npm 用 `NPM_TOKEN`（注入为 `NODE_AUTH_TOKEN`），Python 用 `UV_PUBLISH_TOKEN`，Rust 用 `CARGO_REGISTRY_TOKEN`。

## 项目规范

- [语义化版本](https://semver.org/lang/zh-CN/)
- [约定式提交](https://www.conventionalcommits.org/zh-hans/v1.0.0/)

## License

[MIT](LICENSE)
