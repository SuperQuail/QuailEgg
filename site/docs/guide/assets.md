# 发布资产

除了发布软件包，QuailEgg 还可以把二进制文件、安装包等上传到 GitHub Release，并生成 `checksums.txt`。

这里的“发布资产”指 Release 附件，与 npm、PyPI 或 crates.io 上的软件包发布分别配置。

## 如何选择构建方式

`[release.assets]` 和 `[[release.build]]` 都能为 GitHub Release 准备附件，但构建发生的位置不同：前者让 `qegg release` 在当前机器上构建或收集文件，后者让生成的 GitHub Actions 工作流先判断发布阶段，需要发布时再启动独立的构建 job，把产物交给发布进程。

| 对比项 | `[release.assets]` | `[[release.build]]` |
| --- | --- | --- |
| 配置什么 | 当前发布进程如何构建或收集附件 | 工作流中有哪些独立构建 job |
| 在哪里运行 | 执行 `qegg release` 的机器，本地或 CI 均可 | 各构建 job 的 runner，可分别使用 Windows、macOS、Linux |
| 何时运行 | 实际发布阶段，在软件包发布之前 | PR 模式只在 `publish` 阶段启动，在 `publish` job 之前完成 |
| 如何构建 | Go 交叉编译，或 `commands` 自定义命令 | `steps` 或 `yaml` |
| 如何选择文件 | `files` 匹配当前机器上的文件；Go 模式自动生成归档 | `artifacts` 指定各 job 上传到 Actions 的文件 |
| 如何配置多个平台 | Go 使用 `targets` 在当前机器交叉编译 | 重复添加 `[[release.build]]`，为各 job 指定 runner |
| 本地执行发布时 | 会按资产配置构建或收集 | 不会启动这些 CI job，需要另行准备产物 |

例如，一个不依赖 CGO 的 Go 程序通常可以在 Linux 上一次构建多个平台，使用 `[release.assets]` 即可。Tauri 安装包需要各平台的工具链时，更适合用 `[[release.build]]`，分别在 Windows、macOS 等 runner 上构建。

默认 PR 模式下，这两种方式都会把发布产物的构建留到实际发布时。合并功能 PR、刷新发布 PR 时，不会启动平台构建；发布 PR 合并后才构建，适合安装包等耗时较长的任务。`commit` 模式会在一次运行中直接发布，有发布任务时仍需构建。

如果已有流水线负责构建，也可以直接用 `--assets-dir` 提供产物目录，无需让 QuailEgg 再构建一次。

### 两者如何配合

配置 `[[release.build]]` 后，PR 模式生成的工作流先判断阶段：

```text
plan：判断本次发布阶段
    ├─ version → 更新版本和发布 PR，跳过平台构建
    ├─ none    → 结束，不构建
    └─ publish → 各构建 job 执行 steps 或 yaml
                     ↓
                 按 artifacts 上传文件到 GitHub Actions
                     ↓
                 publish job 下载所有产物
                     ↓
                 qegg release --assets-dir <下载目录>
                     ↓
                 收集文件、生成校验和、上传到 GitHub Release
```

`commit` 模式同样会先判断计划，但只在 `none` 时跳过构建；有任务时，由构建 job 和后续的 `release` job 在同一次运行中完成发布。

这个流程不要求同时写 `[release.assets]`，也不需要在 `files` 中再次列出 `artifacts`。`artifacts` 用于在 Actions job 之间传递文件，真正的 Release 附件仍由 QuailEgg 上传。

如果同时配置了两者，实际发布的 job 传入的 `--assets-dir` 会优先使用预构建产物，跳过 `[release.assets].commands`、`files` 和 Go 交叉编译设置。本地不带 `--assets-dir` 且进入实际发布阶段时，才会按 `[release.assets]` 构建或收集文件。

`[[release.build]]` 只适用于 `managed` 工作流，且不能与 `[release.assets].enabled = false` 同时配置。修改构建 job 后，需要重新运行 `qegg init --force` 才能更新生成的工作流。

## 选择产物来源

| 方式 | 适用情况 | 入口 |
| --- | --- | --- |
| Go 交叉编译 | 不依赖 CGO 的 Go 命令行程序 | `[release.assets]` |
| 自定义构建与收集 | 构建命令可以在 release job 中执行 | `commands` 与 `files` |
| 上传预构建产物 | 各平台分别构建，或已有构建流水线 | `[[release.build]]` 自动传入，或手动指定 `--assets-dir` |

普通配置默认不启用资产。添加资产字段或设置 `enabled = true` 后启用；显式设置 `enabled = false` 则关闭。`--assets-dir` 可以直接指定预构建产物。

多包仓库使用资产时需要 `version_scope = "shared"`，以便为本次发布确定同一个版本和标签。

## Go 交叉编译

对于只有一个 main 包的 Go 项目，可以先使用默认设置：

```toml
[release.assets]
enabled = true
```

默认构建五个平台：

- `linux/amd64`、`linux/arm64`。
- `darwin/amd64`、`darwin/arm64`。
- `windows/amd64`。

构建使用 `CGO_ENABLED=0`、`-trimpath` 和 `-buildvcs=false`，并通过 `-X main.version={{version}}` 注入版本。程序需要定义可写入的 `main.version` 字符串变量，或配置自己的 `ldflags`。

需要指定程序入口或平台时：

```toml
[release.assets]
enabled = true
name = "my-app"
main = "./cmd/my-app"
targets = ["linux/amd64", "windows/amd64"]
ldflags = ["-X main.version={{version}}"]
```

Unix 平台生成 `.tar.gz`，Windows 生成 `.zip`，文件名包含名称、版本、系统和架构。输出位于 `.quailegg/assets/<tag>/`，不提交到仓库。

如果发现多个 main 包，自动探测会提示并跳过构建，需要显式指定 `main`。库项目没有可执行入口时不会构建资产。

## 自定义构建与收集

如果项目有自己的打包命令，可以让 QuailEgg 执行它，再收集生成的文件。下面假设 `npm run package` 会把压缩包写到 `dist/`：

```toml
[release.assets]
enabled = true
commands = ["npm ci", "npm run package"]
files = ["dist/*.zip", "dist/*.tar.gz"]
```

`commands` 在仓库根目录运行，支持 `{{version}}`、`{{tag}}` 占位符，并通过 `QEGG_VERSION`、`QEGG_TAG` 提供版本信息。

`files` 是仓库相对 glob，支持跨目录的 `**`。匹配的文件会被复制到资产输出目录，并生成校验和。已经通过其他步骤构建的文件，也可以只配置 `files`，省略 `commands`。

注意两项配置约束：

- 配置 `commands` 时必须同时提供 `files`。
- `files` 不能与 Go 构建字段 `main`、`targets`、`ldflags` 混用。

## 多平台原生构建

依赖操作系统工具链的应用通常要在各平台分别构建。例如，Rust 二进制可以配置两个 job：

```toml
[[release.build]]
name = "windows"
runner = "windows-latest"
steps = ["cargo build --release --bin my-app"]
artifacts = ["target/release/my-app.exe"]

[[release.build]]
name = "linux"
runner = "ubuntu-latest"
steps = ["cargo build --release --bin my-app"]
artifacts = ["target/release/my-app"]
```

示例假设项目已经登记 Rust 包，且二进制名为 `my-app`。PR 模式下，先由 `plan` job 判断阶段：准备或更新发布 PR 时，两个构建 job 都会跳过；发布 PR 合并后，才从包含版本更新的提交构建产物。

在 `commit` 模式中，这些 job 先于 release job 的版本提升执行。如果产物内嵌版本号，需要自行安排版本注入，或改用版本提升后执行的 `[release.assets].commands`。预构建产物应与最终发布版本一致。

构建成功后，QuailEgg 会把各 job 的产物下载到负责实际发布的 job，再传给 `--assets-dir`。Tauri 等项目可以采用相同方式，但需要补齐前端工具、系统依赖、签名凭证和实际产物路径。

已有项目需要用包含此功能的工具版本重新生成工作流，才能获得按阶段跳过构建的条件。升级步骤见[GitHub Actions](./github-actions#重新生成与升级)。

构建 job 的 `yaml` 接管范围与普通前置步骤不同，详细写法见[GitHub Actions](./github-actions#自定义构建-job-的-yaml)。

## 上传已经构建好的目录

已有构建流水线时，可以直接提供产物目录：

```bash
qegg release --yes --assets-dir ./dist
```

QuailEgg 会收集该目录中的文件，生成校验和，并在发布阶段上传。这个选项替代本地资产构建，但不会跳过版本准备、软件包发布等其他步骤；具体执行阶段仍由当前发布状态决定。

::: warning 输入目录与输出目录分开
不要把 `.quailegg/assets/<tag>/` 作为输入目录。QuailEgg 会重建这个输出目录，输入产物应放在 `dist/` 等单独位置。
:::

## 文件名与目录

收集产物时会按文件名放到同一层级。不同子目录里有同名文件会报错，因此多平台产物应在名字里包含平台信息。

目录本身不会作为一个附件上传。例如，macOS 的 `.app` 是目录，应先打包为 `.zip` 或 `.dmg`；需要保留可执行权限的程序也建议先归档。

生成的 `checksums.txt` 会最后上传。重新上传同名资产时会覆盖已有附件，所以续跑应使用对应版本的产物。

## 跳过构建或上传

- `qegg release --no-assets` 同时跳过资产构建与上传。
- `github_release = false` 或 `--no-github-release` 跳过上传，但仍可能构建资产。
- 上传需要可用的 `gh` 和 GitHub 凭证。
- 使用 `[[release.build]]` 时，不能配置 `[release.assets].enabled = false`。

字段定义见[配置项参考](../reference/config)，中断后的处理见[发布流程](./release-flow#中断后如何继续)。
