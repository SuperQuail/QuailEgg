# 快速上手

## 安装

QuailEgg 需要 Go 1.26 及以上：

```bash
go install github.com/SuperQuail/QuailEgg/cmd/qegg@latest
qegg version
```

确认 `$(go env GOPATH)/bin` 在 `PATH` 中，否则生成的 pre-commit hook 找不到 `qegg`。

发布到 registry 还需要对应工具，例如 `npm`、`uv` 或 `twine`、`cargo`；创建 GitHub Release 需要 `gh`。`qegg check` 会列出缺少的工具。

## 初始化项目

`qegg init` 要求仓库已经配置好远程（默认 `origin`）：仓库身份会写进 `[project] repo`，本地 hook 靠它区分“项目仓库”和 fork 克隆。没有远程时会直接失败：

```bash
git remote add origin https://github.com/you/project
qegg init
```

生成的 hook 只在项目仓库的默认分支上强制变更记录；fork 克隆和普通分支上的提交只提示不拦，合并前的 PR 检查会要求一条记录。

交互向导会检测项目里的包，并询问发布方式、主分支、发布目标等。它会生成：

| 文件 | 作用 |
| --- | --- |
| `.quailegg.toml` | 配置文件 |
| `.changes/README.md` | 变更记录目录与说明 |
| `.git/hooks/pre-commit` | 提交前检查变更记录并运行校验命令 |
| `.github/workflows/qegg-*.yml` | PR 检查、自动发版、状态查询三个工作流 |

在 CI 或脚本里可以跳过交互：

```bash
qegg init --no-input --mode pr --registries npm
```

先看看会写哪些文件，加 `--dry-run`。

## 写第一条变更记录

改完代码后：

```bash
git add -A
qegg commit
```

向导会问变更类型、影响的包和升级级别，然后写入 `.changes/<slug>.md` 并提交。非交互写法：

```bash
qegg commit -m "feat(cache): support configurable ttl" --packages my-package:minor
```

## 预览并发布

```bash
qegg plan       # 看看会升到哪个版本、执行哪些步骤
qegg release    # 执行发布
```

默认的 `pr` 模式下，`qegg release` 会把版本提升与 changelog 放进发布 PR；合并之后再运行一次（通常由 CI 完成）才真正发布。想一步发完，把配置改成：

```toml
[release]
mode = "commit"
```

::: tip
推送到 GitHub 后，`qegg init` 生成的工作流会接管这一切。你只需要写记录、合并 PR。详见 [GitHub Actions](./github-actions)。
:::
