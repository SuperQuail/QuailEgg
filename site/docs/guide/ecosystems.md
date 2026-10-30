# 多语言生态

QuailEgg 根据清单文件识别包，并用对应生态的方式写回版本、发布。

| 生态 | 清单 | Registry | 版本写回 | 发布命令 |
| --- | --- | --- | --- | --- |
| Node.js | `package.json` | npm | `version` 字段 | `npm publish` |
| Python | `pyproject.toml` | PyPI | `[project].version`、`[tool.poetry].version`、hatch/setuptools 动态版本文件 | `uv publish` / `twine upload` |
| Rust | `Cargo.toml` | crates.io | `[package].version` | `cargo publish` |
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
