# 配置项

配置文件为仓库根目录的 `.quailegg.toml`。未知字段会被忽略，保证新版本写出的配置能被旧版本读取。

## 顶层

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `schema_version` | int | `1` | 目前只支持 `1` |

## [project]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `default_branch` | string | `"main"` | 允许发版的分支 |
| `repo` | string | 空 | 项目仓库身份（`host/owner/name`），由 `qegg init` 写入；hook 用它区分项目仓库与 fork 克隆 |

## [git]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `tag_prefix` | string | `"v"` | Tag 前缀，Go 项目必须为 `v`；单包与 shared 版本范围使用 |
| `tag_format` | string | 空 | 独立多包的 tag 模板，默认 `{{slug}}-v{{version}}`，支持 `{{version}}`、`{{slug}}`、`{{name}}` |
| `remote` | string | `"origin"` | 推送使用的 remote |

## [release]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `mode` | string | `"pr"` | `pr` 或 `commit` |
| `branch` | string | `"release"` | pr 模式的发布分支 |
| `github_release` | bool | `true` | 是否创建 GitHub Release |
| `commit_message` | string | `"chore(release): v{{version}}"` | 版本提交信息，支持 `{{version}}`、`{{versions}}`、`{{count}}` |
| `version_scope` | string | `"package"` | `package`（每包独立版本）或 `shared`（全部包同版本；可跨生态，tag 拼写必须一致） |
| `environment` | string | 空 | 发布 workflow 的 GitHub Actions environment 名 |

## [[version.file]]

清单之外还承载版本的文件，例如 Tauri 的 `src-tauri/tauri.conf.json`。`qegg init` 会在生态给出候选时自动登记；表可以重复。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `path` | string | 必填 | 仓库相对路径 |
| `format` | string | `"json"` | 目前只支持 `json` |
| `key` | string | `"version"` | 顶层 JSON 字符串成员 |

版本阶段只改写版本字面量（保留其余字节）并并入 release commit；`qegg check` 会比对文件版本与包版本，漂移直接报错。要求整仓只有一个发布版本：单包仓库，或 `version_scope = "shared"`。

```toml
[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
```

## [release.assets]

Go 包默认由生态交叉编译；`commands` / `files` 用于自定义构建（例如 Tauri 的 `cargo tauri build`）。整表缺省时关闭，写入任意字段即开启。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `enabled` | bool | 关闭 | 显式开关 |
| `name` | string | 包名或仓库名 | 产物名称 |
| `main` | string | 自动探测 `cmd/*` 或根目录 | Go main 包路径，必须在仓库内 |
| `targets` | string[] | 五个常用平台 | Go 的 `os/arch` 列表 |
| `ldflags` | string[] | `["-X main.version={{version}}"]` | Go 链接参数，支持 `{{version}}`、`{{tag}}` |
| `commands` | string[] | `[]` | 发版前在仓库根目录执行的命令，支持 `{{version}}`、`{{tag}}`，并注入 `QEGG_VERSION`、`QEGG_TAG` |
| `files` | string[] | `[]` | 收集的仓库相对 glob（`**` 可用）；一旦写入就不再走生态构建 |

`commands` 必须和 `files` 一起使用；`files` 不能与 `main`、`targets`、`ldflags` 同时出现（自定义收集和 Go 交叉编译是两种模式）。多包仓库只有 `version_scope = "shared"` 才能挂资产（一个 GitHub Release 只能对应一个版本）。

## [release.workflow]

控制 `qegg init` 生成的 `qegg-release.yml`。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `mode` | string | `"managed"` | `managed`（qegg 生成）或 `external`（仓库自己维护发布工作流） |
| `runner` | string | `"ubuntu-latest"` | release job 的 runner |
| `timeout_minutes` | int | `60` | release job 超时（1–1440） |
| `steps` | string[] | `[]` | release job 里 `qegg release` 之前的 shell 步骤 |
| `yaml` | string | 空 | 原样插入的 steps YAML，可写 `uses:`；与 `steps` 互斥 |
| `env` | table | `{}` | shell 步骤的环境变量；`yaml` 模式请在块内自己写 `env:` |

`steps` 和 `yaml` 只能二选一。`external` 模式下只生成 PR 与 status 工作流，发布工作流由仓库自己维护，但仍需调用 `qegg release --yes`。

## [[release.build]]

managed 工作流的多平台构建 job：每个平台一个 job，产物由 release job 下载后交给 qegg 上传。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 必填 | job 名，只能用字母、数字、`-`、`_`；生成 `build-<name>` |
| `runner` | string | 继承 `[release.workflow].runner` | 该平台的 runner |
| `timeout_minutes` | int | 继承 `[release.workflow].timeout_minutes` | 该 job 超时 |
| `steps` | string[] | `[]` | 该 job 的 shell 步骤（qegg 自动加 checkout、setup-go 与各生态 setup） |
| `yaml` | string | 空 | 原样插入的 steps YAML；与 `steps` 互斥，且不再加内置 checkout/setup |
| `env` | table | `{}` | shell 步骤的环境变量；`yaml` 模式请在块内自己写 `env:` |
| `artifacts` | string[] | 必填 | `actions/upload-artifact` 的路径 glob；`yaml` 模式下 qegg 仍自动加这一步 |

每个 build job 必须有 `steps` 或 `yaml` 之一。`yaml` 模式完全接管 steps（qegg 只补 artifact 上传），所以 `checkout`、工具链、构建都要自己写。

```toml
[[release.build]]
name = "windows"
runner = "windows-latest"
steps = ["pnpm -C ui install --frozen-lockfile", "cargo tauri build"]
artifacts = ["target/release/bundle/msi/*.msi"]

[[release.build]]
name = "macos-arm"
runner = "macos-latest"
yaml = """
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }
- uses: pnpm/action-setup@v4
  with: { version: 11 }
- run: pnpm --dir ui install --frozen-lockfile
- run: pnpm --dir ui build
- run: pnpm --dir ui tauri build --target aarch64-apple-darwin
"""
artifacts = ["target/release/bundle/**/*"]
```

存在 `[[release.build]]` 时不能设 `[release.assets].enabled = false`：build job 的产物本来就通过 `--assets-dir` 上传。

## [changes]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `dir` | string | `".changes"` | 变更记录目录 |
| `scope` | string[] | `[]`（整个仓库） | 需要变更记录的目录 |

## [verify]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `commands` | string[] | `[]` | 发版前执行；提交前只在 `[changes].scope` 内有改动时执行 |
| `timeout` | string | `"15m"` | 总超时，Go duration 格式 |
| `fail_fast` | bool | `true` | 第一个失败后停止 |

## [publish]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `registries` | string[] | 按包推断 | `npm`、`pypi`、`crates`、`go` |

## [packages.\<name\>]

`<name>` 是包名，含特殊字符时加引号，例如 `[packages."@acme/core"]`。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `path` | string | 必填 | 包所在目录 |
| `resolver` | string | 必填 | `npm`、`python`、`rust`、`go` |
| `publish` | bool | `true` | 是否发布到 registry |
| `depends-on` | string[] | `[]` | 依赖的其他已登记包 |
