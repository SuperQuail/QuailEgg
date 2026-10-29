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
| `tag_prefix` | string | `"v"` | Tag 前缀，Go 项目必须为 `v` |
| `remote` | string | `"origin"` | 推送使用的 remote |

## [release]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `mode` | string | `"pr"` | `pr` 或 `commit` |
| `branch` | string | `"release"` | pr 模式的发布分支 |
| `github_release` | bool | `true` | 是否创建 GitHub Release |
| `commit_message` | string | `"chore(release): v{{version}}"` | 版本提交信息，支持 `{{version}}` |

## [release.assets]

仅 Go 生态。整个表缺省时关闭，写入任意字段即开启。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `enabled` | bool | 关闭 | 显式开关 |
| `name` | string | main 包目录名 | 产物名称 |
| `main` | string | 自动探测 `cmd/*` 或根目录 | main 包路径，必须在仓库内 |
| `targets` | string[] | 五个常用平台 | `os/arch` 列表 |
| `ldflags` | string[] | `["-X main.version={{version}}"]` | 支持 `{{version}}`、`{{tag}}` |

## [changes]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `dir` | string | `".changes"` | 变更记录目录 |
| `scope` | string[] | `[]`（整个仓库） | 需要变更记录的目录 |

## [verify]

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `commands` | string[] | `[]` | 提交前与发版前执行的命令 |
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
