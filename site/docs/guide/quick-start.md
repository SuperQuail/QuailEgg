# 快速上手

本页会带你为一个已有项目接入 QuailEgg，并通过默认的发布 PR 模式完成一次发布。示例使用名为 `my-package` 的 npm 包；使用其他语言时，步骤相同，工具和凭证见[多语言生态](./ecosystems)。

## 前提条件

开始之前，请确认：

- 项目已经使用 Git，并配置了指向 GitHub 仓库的远程地址。
- 项目有对应的包清单，例如 `package.json`，其中包含包名和当前版本。
- 本地已安装 Go 1.26 或更高版本。
- 你可以向仓库推送分支，并为需要发布的包配置凭证。

后面的示例假设主分支是 `main`，包的当前版本是 `1.2.3`。请把包名和仓库地址替换为自己的值。

## 安装 QuailEgg

```bash
go install github.com/SuperQuail/QuailEgg/cmd/qegg@latest
qegg version
```

第二条命令应输出安装的版本。如果提示找不到 `qegg`，请把 `go env GOBIN` 指向的目录加入 `PATH`；如果该值为空，使用 `go env GOPATH` 下的 `bin` 目录。生成的 Git hook 也需要能找到这个命令。

发布 npm 包需要 `npm`；本地创建发布 PR 或 GitHub Release 还需要 `gh`。你可以在初始化后用 `qegg check` 检查环境。

## 初始化项目

先确认远程地址：

```bash
git remote -v
```

如果尚未配置 `origin`，添加自己的仓库地址：

```bash
git remote add origin https://github.com/you/project.git
```

然后在项目中运行：

```bash
qegg init
```

向导会检测包，并询问主分支、发布方式和发布目标。第一次使用时，可以选择默认的 `pr` 模式。

初始化会生成以下内容：

| 文件 | 用途 |
| --- | --- |
| `.quailegg.toml` | 保存包、发布方式和校验命令等配置 |
| `.changes/README.md` | 说明如何编写变更记录 |
| `.git/hooks/pre-commit` | 检查记录，并在相关文件发生改动时运行校验命令 |
| `.github/workflows/qegg-*.yml` | 检查功能 PR、执行发布和查询发布状态 |
| `.gitignore` 中的托管区块 | 忽略本地运行状态、锁文件和构建产物 |

`qegg init` 会把远程仓库身份写入 `[project].repo`，用于区分项目克隆和 fork 克隆。没有配置远程时，初始化会提示你先添加远程。

::: tip 先查看生成结果
运行 `qegg init --dry-run` 可以预览文件变动。在脚本中使用 `qegg init --no-input` 可以跳过交互。
:::

## 检查并提交初始化结果

打开 `.quailegg.toml`，确认包名、主分支和 `[verify].commands` 符合项目情况，再运行：

```bash
qegg check
```

例如，示例项目应登记为：

```toml
[packages.my-package]
path = "."
resolver = "npm"
```

将初始化生成的文件提交到仓库：

```bash
git add -A
git commit -m "chore: configure releases"
```

按项目的协作方式把这次提交合并到主分支。`.git/hooks/` 不会随 Git 提交；其他协作者需要在自己的克隆中运行 `qegg init`，安装本地 hook。

## 配置 GitHub Actions

对于示例 npm 项目，在仓库的 Actions secrets 中添加 `NPM_TOKEN`。生成的工作流会把它传给 npm 使用的 `NODE_AUTH_TOKEN` 环境变量。

默认的发布 PR 模式还需要在仓库的 **Settings → Actions → General → Workflow permissions** 中允许 GitHub Actions 创建 PR。

其他生态使用的凭证和仓库权限见[GitHub Actions](./github-actions)。如果发布权限由组织统一管理，请先确认这些设置可用，再继续首次发布。

## 提交第一条变更记录

先为这次改动创建功能分支：

```bash
git switch -c feat/cache-expiry
```

现在，假设你为缓存增加了过期时间配置。改完代码后，先暂存，再生成记录：

```bash
git add -A
qegg commit
```

向导会让你选择变更类型、包和升级级别。这个例子可以选择 `feat`、`my-package` 和 `minor`。

你也可以直接提供这些信息：

```bash
qegg commit --no-input -m "feat(cache): support configurable expiration" --body "Allow users to configure cache expiration." --packages my-package:minor
```

`-m` 是 Git 提交标题，`--body` 是进入更新日志的说明。`qegg commit` 会生成并暂存记录，再将它和**已暂存的代码**一起提交，不会自动暂存其他代码改动。

## 预览下一个版本

```bash
qegg plan
```

如果当前版本为 `1.2.3`，且只有这条 `minor` 记录，计划中的下一个版本应为 `1.3.0`。这个命令不会修改文件，也不会发布任何内容。

确认计划后，推送功能分支并发起 PR。生成的 PR 工作流会检查变更记录，并在 PR 中评论发布计划。

## 合并并发布

功能 PR 合并到主分支后，发布工作流会创建一个发布 PR，其中包含版本变更、更新日志和发布标记。这一步不会启动配置的发布产物构建；后续功能 PR 合并时，也只会刷新发布 PR。

检查发布 PR，确认版本号和说明符合预期，然后合并。接下来的工作流才会构建发布产物（如果已配置）、发布软件包、推送标签并创建 GitHub Release。需要长时间打包的项目，也只在这个阶段等待构建，详见[构建触发条件](./github-actions#触发工作流后什么时候才构建)。

发布完成后，可以拉取主分支和标签，再查看状态：

```bash
git pull
git fetch --tags
qegg status
```

你已经完成了首次发布。之后的日常流程就是：**改代码、写记录、合并功能 PR、检查并合并发布 PR**。

## 下一步

- 阅读[变更记录](./change-records)，了解升级级别和提交前检查。
- 阅读[发布流程](./release-flow)，了解直接发布与本地演练。
- 如果工作流失败，查看[问题排查](./faq)。
