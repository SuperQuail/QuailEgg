# 多语言生态

QuailEgg 用同一套变更记录管理不同语言的包，再按各生态的规则读取版本和发布。本页说明支持范围，以及接入时需要注意的差异。

## 支持概览

| 生态 | 包清单 | `resolver` | 发布目标 | 发布方式 |
| --- | --- | --- | --- | --- |
| Node.js | `package.json` | `npm` | `npm` | `npm publish` |
| Python | `pyproject.toml` | `python` | `pypi` | 优先使用 `uv build` 与 `uv publish` |
| Rust | `Cargo.toml` | `rust` | `crates` | `cargo publish` |
| Go | `go.mod` | `go` | `go` | 创建并推送版本标签 |

`qegg init` 用自动发现结果生成建议。初始化后，发布计划主要按配置里登记的包和路径读取清单；不能把自动发现理解为任意嵌套目录都会自动发布。

## Node.js

```toml
[publish]
registries = ["npm"]

[packages.my-package]
path = "."
resolver = "npm"
```

版本写回 `package.json` 的 `version` 字段。npm workspaces 和 `pnpm-workspace.yaml` 中的成员可以被识别；包含版本的 workspace 根包也可能被登记。

发布时直接运行 `npm publish`，QuailEgg 不会额外执行一个通用的前端构建步骤。如果包需要先编译，请使用项目已有的 npm 生命周期脚本或在工作流中明确安排构建。

清单中的 `private: true` 会影响初始化生成的发布开关。对于不需要发布到 npm 的前端或内部包，可以检查配置中是否设置了 `publish = false`。

生成的 Actions 工作流把 Secret `NPM_TOKEN` 映射为 `NODE_AUTH_TOKEN`，并配置 npm registry。使用 pnpm 构建时，还需要自行准备 pnpm 与项目依赖。

预发布会使用渠道 dist-tag，例如 `npm publish --tag alpha`，见[预发布](./prereleases)。

## Python

```toml
[publish]
registries = ["pypi"]

[packages.my-package]
path = "."
resolver = "python"
```

支持读取和写回以下版本来源：

- PEP 621 的 `[project].version`。
- Poetry 的 `[tool.poetry].version`。
- 支持的 hatch/setuptools 动态版本文件。

uv workspace 成员可以自动发现。版本同步会处理支持的兄弟包精确版本依赖；仓库有 `uv.lock` 时，会运行 `uv lock` 更新锁文件。

可用时，QuailEgg 优先执行 `uv build` 和 `uv publish`。没有 uv 时，发布实现使用 `python -m build` 和 `twine upload`，需要自行安装构建工具并配置 twine 认证。生成的工作流使用 uv，`qegg check` 也会检查 uv，因此首次接入建议沿用这条路径。

Python 预发布采用 PEP 440 拼写。例如清单中的 `1.3.0-a1` 对应标签和 PyPI 版本 `1.3.0a1`。

## Rust

```toml
[publish]
registries = ["crates"]

[packages.my-crate]
path = "."
resolver = "rust"
```

普通 crate 的版本写回 `[package].version`。Cargo workspace 的成员也可以被识别，包括只有 `[workspace]` 的虚拟根清单。

### 继承 workspace 版本

成员可以通过 `version.workspace = true` 继承根清单的 `[workspace.package].version`。多个成员共用这个字段时，应选择共享版本：

```toml
[release]
version_scope = "shared"

[packages.app-core]
path = "crates/app-core"
resolver = "rust"

[packages.app-cli]
path = "crates/app-cli"
resolver = "rust"
```

这项限制针对**继承同一根版本的成员**。各成员独立声明版本的 Cargo workspace，并不天然只有一个版本。

当前独立版本模式下，计划包含两个或更多继承版本的成员时，`qegg plan` 会提示，`qegg release` 会拒绝执行。

### 依赖与发布开关

发布准备阶段会同步兄弟 `path` 依赖的版本；支持的内联依赖表缺少 `version` 时会补充。根目录有 `Cargo.lock` 时，会运行 `cargo update --workspace`。

清单中设置 `publish = false` 的 crate，初始化时会生成对应的发布开关。应用项目应确认它没有被误设为发布到 crates.io。

## Go

```toml
[publish]
registries = ["go"]

[packages."github.com/acme/tool"]
path = "."
resolver = "go"
```

Go 模块的版本来自标签，不写回 `go.mod` 中的版本字段。QuailEgg 取可达 HEAD 的最高有效版本标签作为当前版本；没有标签时使用 `0.0.0`，首条 `minor` 记录会得到 `v0.1.0`。

当前支持范围如下：

- `git.tag_prefix` 必须为 `v`。
- 只支持仓库根目录的 `go.mod`，暂不支持子目录模块与 `go.work` 多模块发布。
- Go 包不能加入多包发布计划。
- 发布 v2 或更高版本前，需要自行调整 module 路径和 import，使其带上 `/vN` 后缀；QuailEgg 会检查，不会自动改写。

推送标签后，模块可供 Go 工具链解析。模块代理是否已经缓存该版本是另一项状态，不能只根据代理查询结果判断标签是否发布成功。

如果项目是命令行程序，还可以使用内置的 Go 交叉编译，见[发布资产](./assets)。

## 下一步

- 多个包的版本与发布顺序见[多包仓库](./monorepos)。
- 各生态凭证与工具链准备见[GitHub Actions](./github-actions)。
- 清单与额外版本文件的配置见[配置项参考](../reference/config)。
