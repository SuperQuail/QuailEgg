# 配置项参考

配置文件名为 `.quailegg.toml`。通常由 `qegg init` 创建，再按项目需要调整。配置的基本用法见[配置文件](../guide/configuration)。

未知字段会被忽略，因此请同时检查字段拼写和工具版本；旧版本能读取配置，不代表它支持新字段对应的功能。配置中的包路径、版本文件路径和产物匹配路径均以仓库根目录为基准。

## 顶层

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `schema_version` | int | `1` | 配置格式版本，目前只支持 `1` |

## [project]

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `default_branch` | string | `"main"` | 项目主分支，用于工作流和版本准备阶段的分支检查 |
| `repo` | string | 空 | 项目身份，格式为 `host/owner/name`；由初始化写入，用于 hook 识别 fork 克隆 |

## [git]

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `tag_prefix` | string | `"v"` | 单包或共享版本的标签前缀，Go 项目必须使用 `v` |
| `tag_format` | string | 空 | 独立多包标签模板，留空时使用 `{{slug}}-v{{version}}` |
| `remote` | string | `"origin"` | 推送和仓库身份检查使用的 remote |

`tag_format` 支持 `{{version}}`、`{{slug}}` 和 `{{name}}`。不同版本策略的用法见[多包仓库](../guide/monorepos)。

## [release]

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `mode` | string | `"pr"` | `pr` 为发布 PR，`commit` 为直接提交并发布 |
| `branch` | string | `"release"` | PR 模式的发布分支，不能与主分支相同 |
| `github_release` | bool | `true` | 是否创建 GitHub Release 并上传资产 |
| `commit_message` | string | `"chore(release): v{{version}}"` | 版本提交标题模板 |
| `version_scope` | string | `"package"` | `package` 为独立版本，`shared` 为共享版本 |
| `environment` | string | 空 | release job 使用的 GitHub Actions environment |

`commit_message` 支持 `{{version}}`、`{{versions}}` 和 `{{count}}`。独立多包存在多个不同目标版本时，不应使用单个 `{{version}}` 代表全部版本；初始化会为这种配置选择不含单版本号的标题。

`shared` 要求各包当前版本一致，且版本在标签中的拼写一致。初始化检测到多个同版本包时，可能生成 `shared`，请以文件内容为准。

## [[version.file]]

登记包清单之外的版本文件，可以重复配置：

```toml
[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
```

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `path` | string | 必填 | 仓库内的文件相对路径 |
| `format` | string | `"json"` | 目前只支持 JSON |
| `key` | string | `"version"` | 顶层 JSON 字符串成员名 |

版本准备阶段只改写对应的版本字面量，保留其余内容，并纳入版本提交。`qegg check` 会检查它与包版本是否一致。

此功能要求单包仓库或 `version_scope = "shared"`。`qegg init` 会在生态提供适用候选文件时自动登记。

## [release.assets]

配置 GitHub Release 附件的构建与收集，完整教程见[发布资产](../guide/assets)。

这些设置由 `qegg release` 在当前机器上执行，本地和 CI 都适用。若需要为各平台指定独立 runner，使用 `[[release.build]]`；两者的区别见[如何选择构建方式](../guide/assets#如何选择构建方式)。

整表缺省时关闭；提供资产字段会启用，显式 `enabled = false` 则关闭。

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `enabled` | bool | 默认关闭 | 显式启用或关闭 |
| `name` | string | 按包或仓库推断 | 产物名称 |
| `main` | string | 自动探测 | Go main 包路径，必须位于仓库内 |
| `targets` | string[] | 下方五个平台 | Go 的 `os/arch` 列表 |
| `ldflags` | string[] | `["-X main.version={{version}}"]` | Go 链接参数，支持 `{{version}}`、`{{tag}}` |
| `commands` | string[] | `[]` | 在仓库根目录执行的自定义构建命令 |
| `files` | string[] | `[]` | 收集文件的仓库相对 glob，支持 `**` |

默认目标是 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64` 和 `windows/amd64`。

`commands` 支持 `{{version}}`、`{{tag}}`，并注入 `QEGG_VERSION`、`QEGG_TAG`。有 `commands` 时必须有 `files`；只配置 `files` 可以收集已经构建的文件。

`files` 不能与 `main`、`targets`、`ldflags` 同时设置。多包使用资产时必须选择共享版本。命令行 `--assets-dir` 优先使用预构建目录，并跳过此处的 `commands`、`files` 和 Go 交叉编译。

## [release.workflow]

控制发布工作流的生成。修改后需重新运行 `qegg init --force`，建议先加 `--dry-run` 查看结果。

没有平台构建时，生成单个 `release` job。配置 `[[release.build]]` 后，PR 模式会先运行 `plan`，再分别选择 `version` 或 `publish` job；这两个执行发布命令的 job 都使用下方的前置步骤、环境变量、runner 和超时设置。`plan` 使用同一个 runner，超时固定为 15 分钟，不执行这些自定义前置步骤。

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `mode` | string | `"managed"` | `managed` 为工具生成，`external` 为自行维护 |
| `runner` | string | `"ubuntu-latest"` | release job 的 runner |
| `timeout_minutes` | int | `60` | job 超时，范围为 1–1440 分钟 |
| `steps` | string[] | `[]` | `qegg release` 之前的 shell 步骤 |
| `yaml` | string | 空 | 自定义前置步骤的原始 YAML，可调用 action |
| `env` | table | `{}` | shell 模式的环境变量，不能与 `yaml` 同时使用 |

`steps` 和 `yaml` 互斥。这里的 `yaml` 只替换自定义前置步骤，保留 release job 的内置 checkout、setup、QuailEgg 安装与发布步骤。

PR 模式的版本准备 job 也会执行这些前置步骤。需要按发布阶段执行的耗时构建，应放在 `[[release.build]]`，或放在 `[release.assets].commands`。

`external` 模式不生成发布工作流，仍可生成 PR 和状态工作流；已有发布文件不会自动删除。外部工作流需要自行安装工具并调用发布命令，见[GitHub Actions](../guide/github-actions)。

## [[release.build]]

为 `managed` 工作流增加原生构建 job，可以重复配置。工作流先用 `plan` job 判断阶段，再决定是否启动构建。各 job 上传文件，负责实际发布的 job 下载后通过 `--assets-dir` 交给 QuailEgg。

| 发布模式 | 启动构建的条件 | 后续 job |
| --- | --- | --- |
| `pr` | `phase = publish`；`version` 和 `none` 均跳过 | `publish` 等待构建；`version` 仅依赖计划判断 |
| `commit` | `phase` 不是 `none` | 单个 `release` job 等待构建，完成版本准备和发布 |

这些条件让 PR 模式的主分支推送可以只更新发布 PR，把耗时的平台构建留到实际发布。生成文件中的具体流程见[GitHub Actions](../guide/github-actions#触发工作流后什么时候才构建)。

这组配置在 `qegg init` 生成工作流时生效；本地运行 `qegg release` 不会启动这些 job。已有工作流需要使用包含此功能的工具版本重新生成，才能应用阶段条件。仅配置 `[[release.build]]` 即可，不要求再写 `[release.assets]`；实际发布时，工作流使用 `--assets-dir`，不会再次执行本地资产构建。

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 必填 | 构建名，生成 `build-<name>` job |
| `runner` | string | 继承工作流 runner | 此 job 的 runner |
| `timeout_minutes` | int | 继承工作流超时 | 此 job 的超时 |
| `steps` | string[] | `[]` | shell 步骤，自动补充 checkout、setup-go 和生态 setup |
| `yaml` | string | 空 | 接管构建步骤的原始 YAML，不再自动补充 checkout/setup |
| `env` | table | `{}` | shell 模式的环境变量，不能与 `yaml` 同时使用 |
| `artifacts` | string[] | 必填 | 上传文件的路径 glob |

`name` 以字母或数字开头，其余字符可以是字母、数字、`-` 或 `_`；不同构建名忽略大小写后不能重复。

每个构建必须提供 `steps` 或 `yaml` 之一，两者互斥。`yaml` 模式需要自行写 checkout、工具链和构建，但仍会自动补充 artifact 上传：

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

示例假设 Rust 项目有 `my-app` 二进制。使用构建 job 时，不能设置 `[release.assets].enabled = false`；多包项目仍需共享版本。

## [changes]

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `dir` | string | `".changes"` | 变更记录目录 |
| `scope` | string[] | `[]` | 需要记录、并触发提交前校验的目录 |

空 scope 表示覆盖整个仓库。文档和 CI 是否参与检查同样取决于路径。项目身份、分支条件和托管文件排除规则见[提交前检查](../guide/change-records#提交前会检查什么)。

## [verify]

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `commands` | string[] | `[]` | 准备版本前执行的校验命令 |
| `timeout` | string | `"15m"` | 整组命令的总超时，使用 Go duration 格式 |
| `fail_fast` | bool | `true` | 第一个命令失败后停止 |

安装 hook 后，暂存文件命中 scope 时也会执行这些命令。`qegg verify` 可手动运行，不要求存在暂存改动。

## [publish]

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `registries` | string[] | 按包推断 | 发布目标，支持 `npm`、`pypi`、`crates`、`go` |

初始化渲染工作流时，显式发布目标优先决定生态 setup 和凭证；没有目标时再参考包的 resolver。跨生态项目应检查生成工作流是否覆盖所需的工具链。

## [packages.<name>]

每个包登记一个表。名称含特殊字符时加引号，例如：

```toml
[packages."@acme/core"]
path = "packages/core"
resolver = "npm"
publish = true
```

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `path` | string | 必填 | 包所在目录，根包使用 `"."` |
| `resolver` | string | 必填 | `npm`、`python`、`rust` 或 `go` |
| `publish` | bool | `true` | 是否发布软件包；关闭后仍可管理版本 |
| `depends-on` | string[] | `[]` | 其他已登记包的依赖关系，用于发布排序 |

包名应与实际清单一致。`depends-on` 不会自动为依赖方创建升级记录；多个包的输入示例见[多包输入](./cli#多包输入)。
