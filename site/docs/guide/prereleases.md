# 预发布

在发布稳定版之前，你可能希望先让用户试用新功能。QuailEgg 支持 `alpha`、`beta` 和 `rc` 三个预发布渠道，使用方式与稳定版相同：写记录、预览，再通过发布流程发布。

本页以 npm 包从 `1.2.3` 升级到 `1.3.0` 为例。

## 进入预发布渠道

在已配置 QuailEgg 的仓库中运行：

```bash
qegg pre alpha
```

命令会写入 `.quailegg/pre-release.lock`。这是需要提交的渠道状态，不是本地临时文件：

```bash
git add .quailegg/pre-release.lock
git commit -m "chore: start alpha releases"
```

按项目的协作流程将这个提交合并到主分支。如果还没有变更记录，进入渠道本身不会创建一个新版本。

## 准备第一个预发布

提交一条新功能记录后，运行：

```bash
qegg pre
qegg plan
```

`qegg pre` 不带参数时只显示当前渠道和下一个版本。假设当前稳定版是 `1.2.3`，记录要求 `minor`，第一个版本就是：

```text
1.2.3 → 1.3.0-alpha.1
```

继续使用原有的发布方式。默认 PR 模式仍然需要先合并发布 PR，再实际发布。

## 继续发布同一渠道

试用过程中有新改动时，像平时一样提交代码和记录。下一次发布会增加编号：

```text
1.3.0-alpha.1 → 1.3.0-alpha.2
```

版本由稳定版本基线、记录要求的升级级别和已有渠道编号共同决定。如果记录使目标基线提高，新的基线从编号 `1` 开始。用 `qegg plan` 查看实际版本，不需要手动改编号。

预发布期间：

- 变更记录会保留，供稳定版汇总。
- `CHANGELOG.md` 不会更新。
- 本渠道的 Release notes 只列出上一次预发布之后新增的记录；稳定版才汇总自上一个稳定 tag 以来的全部记录，compare 链接也从该 tag 起算。
- 没有新的记录变化时，重复运行不会只为了增加编号而发布。

## 切换渠道

已经处于一个渠道时，不能直接进入另一个渠道。先退出，再进入新渠道：

```bash
qegg pre exit
qegg pre beta
qegg plan
```

确认结果后，将锁文件的变化与后续改动一起提交。每次切换都应检查计划，尤其是在目标版本也发生变化时。

## 发布稳定版

准备好稳定版后，退出渠道：

```bash
qegg pre exit
git add -A .quailegg/pre-release.lock
git commit -m "chore: finish prereleases"
```

`exit` 会删除渠道锁文件。上面的 `git add -A` 会暂存这次删除；只在本地运行 `exit` 不会让 CI 退出渠道。

将提交合并到主分支后，继续正常发布流程。如果目标基线未变，版本会从：

```text
1.3.0-alpha.2 → 1.3.0
```

稳定版会一次性更新 `CHANGELOG.md`，并删除已经收录的变更记录。稳定版的 Full Changelog 从最后一个稳定 tag 起算，正文汇总该区间的全部记录，而不是只汇总最后一个预发布之后的增量。

## 不同发布目标的表现

| 发布目标 | 预发布行为 |
| --- | --- |
| npm | 使用同名 dist-tag，例如 `alpha`、`beta` 或 `rc`，不会覆盖 `latest` |
| Python | 使用 Python 版本写法，例如清单中的 `1.3.0-a1` 对应标签和 PyPI 版本 `1.3.0a1` |
| crates.io 与 Go | 使用带预发布后缀的版本或标签 |
| GitHub Release | 标记为预发布，且不会设为 Latest |

Python 和其他生态的预发布标签拼写不同，跨生态共用版本时需要留意这一限制，见[多包仓库](./monorepos#跨生态共用版本)。

完整命令用法见[命令行参考](../reference/cli#qegg-pre)。
