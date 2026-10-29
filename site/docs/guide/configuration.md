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

每个包单独登记，`path` 指向包所在目录：

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

## 凭证

发布凭证不写进配置，统一走环境变量或 CI Secrets：

| 生态 | 环境变量 |
| --- | --- |
| npm | `NODE_AUTH_TOKEN` |
| Python | `UV_PUBLISH_TOKEN` |
| Rust | `CARGO_REGISTRY_TOKEN` |
| GitHub Release | `GITHUB_TOKEN` |

完整字段见 [配置项参考](../reference/config)。
