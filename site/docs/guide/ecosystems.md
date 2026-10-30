# 多语言生态

QuailEgg 根据清单文件识别包，并用对应生态的方式写回版本、发布。

| 生态 | 清单 | Registry | 版本写回 | 发布命令 |
| --- | --- | --- | --- | --- |
| Node.js | `package.json` | npm | `version` 字段 | `npm publish` |
| Python | `pyproject.toml` | PyPI | `[project].version`、`[tool.poetry].version`、hatch/setuptools 动态版本文件 | `uv publish` / `twine upload` |
| Rust | `Cargo.toml` | crates.io | `[package].version`；继承时写根 `[workspace.package].version` | `cargo publish` |
| Go | `go.mod` | proxy.golang.org | 无，版本来自 git tag | 无，push tag 即发布 |

npm/pnpm、uv、cargo 的多包仓库都能被识别，可以按包独立发版，也可以用 `version_scope = "shared"` 统一版本。

## Node.js

```toml
[publish]
registries = ["npm"]

[packages.my-package]
path = "."
resolver = "npm"
```

## Python

```toml
[publish]
registries = ["pypi"]

[packages.my-package]
path = "."
resolver = "python"
```

## Rust

```toml
[publish]
registries = ["crates"]

[packages.my-crate]
path = "."
resolver = "rust"
```

Cargo workspace 也能直接识别：根清单是虚拟清单（只有 `[workspace]`）时，成员的 `version.workspace = true` 会解析成根 `[workspace.package].version`。一个 workspace 只有一个版本，所以继承版本的仓库要用 `version_scope = "shared"`：

```toml
[release]
version_scope = "shared"

[publish]
registries = ["crates"]

[packages.sc2mpq]
path = "crates/sc2mpq"
resolver = "rust"

[packages.sc2trans-core]
path = "crates/sc2trans-core"
resolver = "rust"
```

发布时 qegg 写根版本、同步兄弟 `path` 依赖的 `version`（缺 `version` 的内联表会补上，否则 `cargo publish` 会拒绝），并在根目录有 `Cargo.lock` 时执行 `cargo update --workspace`。`[package].publish = false` 的 crate 不发布，`qegg init` 会把它写进 `[packages.*].publish`。package scope 下有两个及以上继承成员时，`qegg plan` 会警告、`qegg release` 会报错。

## Go

```toml
[publish]
registries = ["go"]

[packages."github.com/acme/tool"]
path = "."
resolver = "go"
```

Go module 没有版本字段，版本就是可达 HEAD 的最高 `v*` tag，因此有几条额外约束：

- `git.tag_prefix` 必须是 `v`；
- 只支持仓库根目录的 `go.mod`，暂不支持子目录模块与 `go.work`；
- v2 及以上要求 module 路径带 `/vN` 后缀，qegg 在 plan 阶段报错并给出期望路径，不会自动改写；
- 没有 tag 时以 `0.0.0` 为基线，首个 `minor` 记录得到 `v0.1.0`。

Go 项目还可以构建 [发布资产](./assets)。
