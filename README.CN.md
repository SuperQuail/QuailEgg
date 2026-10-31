# QuailEgg

[English](README.md) | 中文

QuailEgg 是一个自动发版工具。合并到主分支后，它汇总 `.changes/` 变更记录，计算版本号，更新 `CHANGELOG.md`，发布软件包并创建 Git Tag 与 GitHub Release。

## 特性

- 变更记录驱动发版，pre-commit hook 检查记录并执行 `[verify].commands`
- 自动识别 npm、PyPI、crates.io、Go module、Maven 与 Gradle 工程，并扫描根目录下最多三层的包清单
- 发布方式可选：发布 PR 或直接提交，失败可断点续跑
- 预发布渠道（`alpha`、`beta`、`rc`），记录留到稳定版一起发布
- `qegg init` 生成 GitHub Actions 工作流，合并即自动发版

## 安装

```bash
go install github.com/SuperQuail/QuailEgg/cmd/qegg@latest
```

## 快速开始

```bash
qegg scan       # 查看 init 可以登记的包
qegg init       # 生成配置、git hook 和 GitHub Actions 工作流
qegg commit     # 写一条变更记录并提交
qegg release    # 发版
```

`qegg init` 要求仓库已经配置好 `origin` 远程：仓库身份会写进 `[project] repo`，pre-commit hook 靠它区分 fork 克隆。

默认用发布 PR；想直接提交版本，把 `.quailegg.toml` 里的 `[release].mode` 改成 `commit`。

## 预发布

```bash
qegg pre alpha      # 开启 alpha 渠道
qegg pre            # 查看当前渠道与下一个版本
qegg pre exit       # 毕业，发布稳定版
```

渠道状态写在随仓库提交的 `.quailegg/pre-release.lock` 里。预发布不写 `CHANGELOG.md`、不消费变更记录；`qegg pre exit` 之后的稳定版会一次性写入并消费。GitHub Release 会标记为 Pre-release，npm 发布到同名 dist-tag。

## 命令

| 命令 | 说明 |
| --- | --- |
| `qegg init` | 生成配置、hook 和工作流 |
| `qegg scan` | 列出 init 可以登记的包 |
| `qegg commit` | 生成变更记录并提交 |
| `qegg check` | 检查环境与配置 |
| `qegg plan` | 预览发版计划 |
| `qegg verify` | 执行发版前校验命令 |
| `qegg changelog` | 根据 `.changes/` 生成 CHANGELOG |
| `qegg pre` | 开启、退出或查看预发布渠道 |
| `qegg release` | 执行发布 |
| `qegg resume` | 恢复中断的发布 |
| `qegg status` | 查看发布状态 |
| `qegg lang` | 设置界面语言（默认英文） |

## 变更记录

每次改动在 `.changes/` 下加一个 Markdown 文件：

```markdown
---
type: feat
packages:
  my-package: minor
---

用一两句话说明这次改动。
```

`qegg commit` 会帮你生成。完整字段与写作规范见 [AGENTS.md](AGENTS.md)。

## 配置

在项目根目录创建 `.quailegg.toml`：

```toml
schema_version = 1

[project]
default_branch = "main"
repo = "github.com/you/project"   # 由 qegg init 写入

[publish]
registries = ["npm"]

[packages."npm@my-package"]
name = "my-package"
path = "."
resolver = "npm"    # npm | python | rust | go | java
```

包表名是包的身份键：新配置使用 `resolver@清单名`，并用 `name` 记录真实清单名；旧配置可以继续用清单名作键。未填写的配置项使用自动检测结果或默认值；发布凭证走环境变量或 CI Secrets。

## License

[MIT](LICENSE)
