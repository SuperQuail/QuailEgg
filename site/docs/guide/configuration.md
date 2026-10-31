# 配置文件

`.quailegg.toml` 描述项目有哪些包，以及如何准备和发布版本。通常先运行 `qegg init` 生成配置，再按项目需要调整。

本页介绍常用选择。所有字段的类型、默认值和约束见[配置项参考](../reference/config)。

## 找到配置

QuailEgg 从当前目录向上查找 `.quailegg.toml`。你也可以指定工作目录或配置路径：

```bash
qegg --cwd ./project plan
qegg --config .quailegg.local.toml plan
```

未知字段会被忽略，但这不代表旧版本会执行新字段对应的功能。修改配置时，请同时确认本地和 CI 使用的 QuailEgg 版本。

## 从一个包开始

一个 npm 包的最小配置如下：

```toml title=".quailegg.toml"
schema_version = 1

[packages.my-package]
path = "."
resolver = "npm"
```

`my-package` 应与包清单中的名字一致。`path` 相对于仓库根目录，`resolver` 指定读取清单和发布时使用的生态。

其余字段使用默认值。不过，首次接入仍建议用 `qegg init`：它还会记录仓库身份，并生成 hook 和工作流。

## 选择发布方式

```toml
[release]
mode = "pr"
branch = "release"
github_release = true
```

`pr` 是默认模式，会先创建发布 PR，合并后再发布。需要一次运行直接完成发布时，改用 `commit`。两者的执行过程见[发布流程](./release-flow)。

主分支和 Git remote 可以单独配置：

```toml
[project]
default_branch = "main"
repo = "github.com/you/project"

[git]
remote = "origin"
tag_prefix = "v"
```

`repo` 是 `qegg init` 写入的项目身份，用于本地 hook 识别 fork 克隆；它不是认证凭证。

## 指定哪些改动需要记录

```toml
[changes]
dir = ".changes"
scope = ["src", "lib"]
```

这个例子要求 `src/` 和 `lib/` 下的改动附带记录。`scope = []` 表示整个仓库，而不是关闭检查。

文档或 CI 改动是否需要记录，取决于它们是否位于配置范围内。记录检查和校验命令的生效条件见[变更记录](./change-records#提交前会检查什么)。

## 配置校验命令

```toml
[verify]
commands = ["npm test"]
timeout = "15m"
fail_fast = true
```

QuailEgg 在准备版本之前执行这些命令。安装本地 hook 后，暂存区命中 `[changes].scope` 时也会执行它们。

请把示例中的 `npm test` 换成项目实际可用的命令。`timeout` 是整组命令的总超时；`fail_fast = true` 表示遇到第一个失败就停止。

配置后可以手动检查：

```bash
qegg verify
```

## 配置发布目标与凭证

```toml
[publish]
registries = ["npm"]
```

发布目标使用 `npm`、`pypi`、`crates` 或 `go`。注意，它们与 `resolver` 的名字不完全相同，例如 Python 使用 `resolver = "python"` 和 `registries = ["pypi"]`。

凭证通过环境变量或 CI secrets 提供：

| 用途 | 发布进程使用的变量 | 生成的 Actions 工作流读取的 Secret |
| --- | --- | --- |
| npm | `NODE_AUTH_TOKEN` | `NPM_TOKEN` |
| Python（uv） | `UV_PUBLISH_TOKEN` | `UV_PUBLISH_TOKEN` |
| Rust | `CARGO_REGISTRY_TOKEN` | `CARGO_REGISTRY_TOKEN` |
| GitHub PR 与 Release | `GITHUB_TOKEN`，或已登录的 `gh` | Actions 自动提供的 `GITHUB_TOKEN` |

npm 的环境变量还需要相应的 npm 认证配置；生成的 Actions 工作流会通过 setup-node 配置它。本地使用时可沿用已有的 npm 登录与认证配置。

## 多包仓库

每个包登记一个 `[packages."<key>"]` 表。新配置的键是 `resolver@清单名`（如 `npm@my-package`），并用 `name` 记录真实清单名；旧式清单名键仍然有效。默认配置值 `version_scope = "package"` 表示独立版本，`shared` 表示所有包共用版本。

首次初始化检测到多个包版本一致时，`qegg init` 可能会建议并生成 `shared`，因此请以生成的配置为准。完整示例见[多包仓库](./monorepos)。

## 同步额外的版本文件

应用可能还需要在包清单之外保存版本，例如 Tauri 的配置文件：

```toml
[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
```

QuailEgg 会在准备版本时同步这个顶层 JSON 字符串字段，并将改动一起提交。`qegg check` 会检查它与包版本是否一致。

额外版本文件只适用于单包或 `shared` 仓库，因为一个文件只能对应一个发布版本。Tauri 的完整配置见[多包仓库](./monorepos#跨生态共用版本)。

## 更新生成的工作流

修改 `[release.workflow]`、`[[release.build]]` 或其他影响工作流的配置后，需要重新生成文件：

```bash
qegg init --dry-run --force
qegg init --force
```

先预览，再检查生成文件的差异并提交。没有 `--force` 时，已经存在的 hook 和工作流会被保留。

如果你自行维护发布工作流，使用 `[release.workflow].mode = "external"`。相关示例见[GitHub Actions](./github-actions)。
