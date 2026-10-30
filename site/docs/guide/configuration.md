# 配置文件

QuailEgg 从当前目录向上查找 `.quailegg.toml`，也可以用 `--config` 指定。未知字段会被忽略，这样新版本写出的配置，仍能被本地尚未升级的旧版本读取。

## 最小配置

```toml title=".quailegg.toml"
schema_version = 1

[packages.my-package]
path = "."
resolver = "npm"
```

其余项都有默认值。`qegg init` 会把检测到的包写进 `[packages]`。

## 常用配置

```toml title=".quailegg.toml"
schema_version = 1

[project]
default_branch = "main"       # 只允许在这个分支发版
repo = "github.com/you/project"  # 项目仓库身份，qegg init 写入，用于识别 fork 克隆

[git]
tag_prefix = "v"
remote = "origin"

[release]
mode = "pr"                   # pr | commit
branch = "release"            # pr 模式下发布 PR 的分支
github_release = true
commit_message = "chore(release): v{{version}}"

[changes]
dir = ".changes"
scope = ["src", "lib"]        # 这些目录的改动需要变更记录

[verify]
commands = ["npm test"]       # 发版前与提交前执行
timeout = "15m"
fail_fast = true

[publish]
registries = ["npm"]          # npm | pypi | crates | go

[packages.my-package]
path = "."
resolver = "npm"              # npm | python | rust | go
```

## 多包仓库

每个包单独登记，`path` 指向包所在目录。默认每个包按自己的记录独立发版；要让所有包共用一个版本，加 `version_scope = "shared"`：

```toml
[packages."@acme/core"]
path = "packages/core"
resolver = "npm"

[packages."@acme/cli"]
path = "packages/cli"
resolver = "npm"
depends-on = ["@acme/core"]

[packages.internal-tools]
path = "tools"
resolver = "npm"
publish = false               # 提升版本，但不发布到 registry
```

检测到未登记的包时，`qegg plan` 会给出警告，运行 `qegg init` 可以补充登记。

独立发版的 tag 默认是 `core-v1.2.3`（`[git] tag_format`，支持 `{{version}}`、`{{slug}}`、`{{name}}`）；shared 版本范围沿用 `tag_prefix`，所有包共用 `v1.2.3`。发布顺序按清单依赖和 `depends-on` 决定，被依赖的包先发布。

Cargo workspace 的成员用 `version.workspace = true` 继承根版本时只能选 `shared`：Cargo 一个 workspace 只有一个版本，package scope 下有两个继承成员会在 release 时报错。

```toml
[release]
version_scope = "shared"   # package（默认）| shared
environment = "pypi"       # 渲染进发布 workflow 的 environment
```

## Tauri / 跨生态同版本

Tauri 仓库通常是「Rust crates + 前端 + `src-tauri`」共用一个应用版本。发现只扫仓库根目录，Rust workspace 会被自动登记，前端 `ui/` 需要手写一条包配置：

```toml
[release]
version_scope = "shared"

[packages."sc2translater-ui"]
path = "ui"
resolver = "npm"
publish = false

[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
```

shared 允许跨生态，但 tag 拼写必须一致；Python 的预发布是 PEP 440 拼写，不能和其他生态共享预发布。`qegg init` 会自动登记 `tauri.conf.json`，版本阶段把它和清单一起改写，`qegg check` 会检查漂移。

多平台安装包用 `[[release.build]]`，见 [GitHub Actions](github-actions) 与 [发布资产](assets)。

## 凭证

发布凭证不写进配置，统一走环境变量或 CI Secrets：

| 生态 | 环境变量 |
| --- | --- |
| npm | `NODE_AUTH_TOKEN` |
| Python | `UV_PUBLISH_TOKEN` |
| Rust | `CARGO_REGISTRY_TOKEN` |
| GitHub Release | `GITHUB_TOKEN` |

完整字段见 [配置项参考](../reference/config)。
