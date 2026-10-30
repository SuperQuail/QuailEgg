# GitHub Actions

`qegg init` 可以生成 GitHub Actions 工作流，让检查和发布随 PR 合并自动进行。本页先介绍默认流程，再说明如何添加项目自己的安装、构建和发布设置。

## 默认生成的工作流

| 工作流 | 触发方式 | 用途 |
| --- | --- | --- |
| `qegg-pr.yml` | 向主分支发起或更新功能 PR | 检查变更记录，并评论发布计划 |
| `qegg-release.yml` | 主分支上的发布相关路径发生变化，或手动触发 | 运行 `qegg release --yes` |
| `qegg-status.yml` | 手动触发 | 输出 `qegg status --json` |

默认 PR 模式会先创建发布 PR，合并后再发布。流程见[发布流程](./release-flow)。

如果不需要状态查询工作流，初始化时加 `--no-status-workflow`；完全不生成工作流则使用 `--no-workflows`。

## 首次接入需要做什么

将生成的配置和工作流提交到主分支，然后完成以下设置：

1. 在仓库的 **Settings → Secrets and variables → Actions** 中添加发布凭证。
2. 使用 PR 模式时，在 **Settings → Actions → General → Workflow permissions** 中允许 Actions 创建 PR。
3. 检查分支保护是否允许所选发布模式完成推送和合并。
4. 如果校验依赖已经安装的依赖包，为 release job 添加安装步骤。

### 发布凭证

| 发布目标 | 仓库 Secret | 传入发布进程的变量 |
| --- | --- | --- |
| npm | `NPM_TOKEN` | `NODE_AUTH_TOKEN` |
| PyPI（uv） | `UV_PUBLISH_TOKEN` | `UV_PUBLISH_TOKEN` |
| crates.io | `CARGO_REGISTRY_TOKEN` | `CARGO_REGISTRY_TOKEN` |
| Go 模块 | 不需要 registry 凭证 | 通过 Git 标签发布 |

GitHub PR 与 Release 使用 Actions 自动提供的 `GITHUB_TOKEN`。生成的发布工作流声明了写入仓库内容和 PR 的权限，但组织策略与分支保护仍可能限制具体操作。

设置了 `[release].environment` 时，release job 会使用对应的 GitHub Actions environment。请确认凭证位于该 job 可以访问的位置。

### 安装项目依赖

生成的工作流会安装 Go、QuailEgg 和所需生态的工具链，但不会替你完成所有项目依赖安装。例如，校验命令使用 `npm test` 时，可以配置：

```toml
[release.workflow]
steps = ["npm ci"]
```

这里假设项目已经提交 `package-lock.json`。pnpm、Python 或其他项目应换成自己的安装命令，并准备相应工具。

## 哪些改动会触发发布

生成的发布工作流会监听：

- 变更记录目录，默认是 `.changes/**`。
- `.quailegg.toml`、预发布渠道锁文件和发布标记。
- `CHANGELOG.md`。
- 已登记包对应的清单路径。

因此，只有文档变化、又没有改动这些路径时，不会启动发布。修改包路径、记录目录等配置后，需要重新生成工作流，才能更新路径过滤规则。

工作流还会跳过 `github-actions[bot]` 发起的运行，避免生成的版本提交循环触发。

## 发布 PR 的检查

发布 PR 由自动化账号创建，并由 QuailEgg 管理。生成的 `qegg-pr` 检查会跳过配置的发布分支；请不要要求这个发布 PR 等待一项被跳过的检查。

功能 PR 的记录检查发生在合并前。发布 PR 中的版本变化会在准备版本时经过配置的校验；合并后，发布阶段继续检查标记和发布状态。

如果发布 PR 被仓库的必需检查或审批规则阻止，请查看具体规则和 workflow 日志。不要仅凭一项缺失的检查判断软件包已经发布。

## 定制 release job

`[release.workflow]` 可以修改 runner、超时和 `qegg release` 之前的步骤：

```toml
[release.workflow]
runner = "ubuntu-latest"
timeout_minutes = 60
steps = ["npm ci"]
```

需要调用 action 时，用 `yaml` 代替 `steps`：

```toml
[release.workflow]
yaml = """
- name: Install dependencies
  run: npm ci
"""
```

这里的 `yaml` 只替换**自定义前置步骤**。release job 中内置的 checkout、工具链安装、QuailEgg 安装和最终发布步骤仍会保留。

`steps` 与 `yaml` 不能同时设置。shell 步骤的变量可以写在 `env` 中；使用 `yaml` 时，把 `env:` 写进 YAML 步骤。

## 多平台构建 job

`[release.assets]` 配置的是 `qegg release` 在当前机器上的资产构建与收集；`[[release.build]]` 配置的是发布之前独立运行的 CI job。只有后者可以为各 job 分别选择 runner。如何选择与同时配置时的优先级，见[发布资产](./assets#如何选择构建方式)。

需要在各个平台原生构建的程序，可以用 `[[release.build]]` 增加构建 job。下面假设 Rust 项目有一个名为 `my-app` 的二进制：

```toml
[[release.build]]
name = "windows"
runner = "windows-latest"
steps = ["cargo build --release --bin my-app"]
artifacts = ["target/release/my-app.exe"]
```

shell 模式下，QuailEgg 会添加 checkout、setup-go 和项目生态的 setup，然后执行 `steps` 并上传匹配的文件。

release job 会等待构建完成，下载全部产物，再执行：

```bash
qegg release --yes --assets-dir .quailegg/assets/downloaded
```

因此，只使用 `[[release.build]]` 就可以把产物交给 QuailEgg 上传，不必再配置 `[release.assets].commands` 或 `files`。传入 `--assets-dir` 时，也不会再次执行这两项配置。

产物收集、文件命名和共享版本要求见[发布资产](./assets)。

### 自定义构建 job 的 YAML

与 release job 的前置步骤不同，`[[release.build]].yaml` 会接管该构建 job 的全部自定义步骤。你需要自行写 checkout、工具链和构建；QuailEgg 只补上 artifact 上传。

```toml
[[release.build]]
name = "linux"
runner = "ubuntu-latest"
yaml = """
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }
- uses: dtolnay/rust-toolchain@stable
- run: cargo build --release --bin my-app
"""
artifacts = ["target/release/my-app"]
```

每个构建 job 必须指定 `steps` 或 `yaml` 之一，并提供 `artifacts`。只要使用了 `[[release.build]]`，就不能显式关闭发布资产。

## 自行维护发布工作流

已有发布流水线的项目可以设置：

```toml
[release.workflow]
mode = "external"
```

之后 `qegg init` 不再生成发布工作流，仍可生成 PR 和状态查询工作流。已经存在的发布工作流不会被自动删除，需要自行检查并接管。

外部工作流负责检出代码、安装工具、准备依赖和凭证，再调用 `qegg release --yes`。预构建产物通过 `--assets-dir` 传入。`[[release.build]]` 只用于 `managed` 模式。

如果使用其他 action 构建安装包，应明确由谁管理标签和 GitHub Release，避免两个发布流程重复创建同一个版本。

## 重新生成与升级

修改工作流相关配置后，先查看预期差异：

```bash
qegg init --dry-run --force
qegg init --force
```

生成的工作流通常固定使用生成时的已发布 QuailEgg 版本。升级本地工具后，可以重新生成，或用 `--qegg-version` 指定一个已发布版本。

请检查并提交生成文件的差异。如果使用开发构建或显式指定 `latest`，生成的安装命令可能不固定版本；运行初始化时会提示这一点。
