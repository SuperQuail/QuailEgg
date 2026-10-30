# 简介

QuailEgg（命令名 `qegg`）是一个由变更记录驱动的自动发布工具。你在提交改动时写下“这次改了什么、应该提升哪个版本”，QuailEgg 会在发布时汇总这些记录，更新版本号和更新日志，再发布软件包、推送 Git 标签并创建 GitHub Release。

它支持 npm、PyPI、crates.io 和 Go 模块，也可以为应用上传二进制文件和安装包。

## 从一条记录开始

假设你的包当前是 `1.2.3`，这次为缓存增加了过期时间配置。你可以为它写一条记录：

```markdown title=".changes/cache-expiry.md"
---
type: feat
scope: cache
packages:
  my-package: minor
---

Allow users to configure cache expiration.
```

这里的 `minor` 表示这是一项向后兼容的新功能。运行 `qegg plan` 后，你会看到下一个版本是 `1.3.0`；记录正文会成为更新日志中的一条说明。

你也可以用 `qegg commit` 生成记录，暂时不需要记住所有字段。后面的[变更记录](./change-records)会介绍它们的具体用途。

## 一次发布是怎样完成的

默认情况下，QuailEgg 使用发布 PR 来准备新版本：

1. 你在功能 PR 中提交代码和变更记录。
2. 功能 PR 合并后，工作流汇总待发布记录，创建或更新发布 PR。
3. 你检查版本号和更新日志，合并发布 PR。
4. 工作流发布软件包，推送标签并创建 GitHub Release。

因此，功能 PR 合并后并不会立即发布软件包。发布 PR 为你保留了一次检查发布内容的机会。如果项目希望直接发布，也可以选择 `commit` 模式。两种方式的区别见[发布流程](./release-flow)。

## 你会遇到的几个概念

| 概念 | 含义 |
| --- | --- |
| 变更记录 | 默认放在 `.changes/` 下，描述改动及其版本升级级别 |
| 发布计划 | 根据待发布记录计算的版本、受影响的包和执行步骤，用 `qegg plan` 查看 |
| 发布标记 | 提交到仓库的 `.quailegg/release.json`，记录已准备好、等待发布的版本 |
| 运行记录 | 保存在本地 `.quailegg/state/`，用于查看执行过程，不提交到仓库 |
| 预发布渠道 | 在稳定版之前发布 `alpha`、`beta` 或 `rc` 版本 |

发布标记让准备版本和实际发布可以在两次工作流中完成。发生中断时，QuailEgg 会根据仓库和发布目标的状态重新判断接下来要做什么。

## 选择接下来的阅读路径

- **第一次使用**：从[快速上手](./quick-start)开始，为一个已有项目接入发布流程。
- **想了解日常用法**：继续阅读[变更记录](./change-records)和[发布流程](./release-flow)。
- **维护多个包或应用**：查看[多包仓库](./monorepos)、[多语言生态](./ecosystems)和[发布资产](./assets)。
- **查找具体选项**：直接使用[命令行参考](../reference/cli)和[配置项参考](../reference/config)。
