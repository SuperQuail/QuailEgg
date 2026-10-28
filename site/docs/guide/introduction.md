# 简介

QuailEgg（命令名 `qegg`）是一个自动发版工具。改动合并到主分支后，它汇总 `.changes/` 里的变更记录，计算下一个版本号，更新 `CHANGELOG.md`，发布软件包，并创建 Git Tag 与 GitHub Release。

## 它解决什么问题

手动发版通常要记住一串步骤：改版本号、整理 changelog、打 tag、推送、发 registry、写 Release 说明。任何一步漏掉或失败都会留下不一致的状态。

QuailEgg 把"这次改了什么、该升哪一级"前移到写代码的时候：

- 每个改动附一条 Markdown 记录，写清类型、影响的包和升级级别；
- 发版时，qegg 根据所有待发记录算出版本，生成 changelog，按固定顺序执行发布；
- 每一步的结果都持久化，失败后可以安全续跑。

如果你用过 [Changesets](https://github.com/changesets/changesets)，会觉得很熟悉。区别在于 QuailEgg 不局限于 JavaScript，同一套流程覆盖 npm、PyPI、crates.io 与 Go module。

## 核心概念

- 变更记录：`.changes/` 下的一个 Markdown 文件，描述一次改动。详见 [变更记录](./change-records)。
- 发布计划：qegg 从待发记录推导出的版本变化与步骤清单，可以用 `qegg plan` 随时预览。
- 发布标记：`.quailegg/release.json`，记录"已经提升版本、等待发布"的版本，用来衔接 pr 模式的两个阶段。
- 发布方式：`pr`（默认）先开发布 PR，合并后再发布；`commit` 直接提交版本并发布。详见 [发布流程](./release-flow)。

## 下一步

- 想马上用起来，看 [快速上手](./quick-start)。
- 想了解每个命令与参数，看 [命令行参考](../reference/cli)。
