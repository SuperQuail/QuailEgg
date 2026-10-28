# 发布流程

## 两个阶段

一次发布分成两个阶段：

1. 提升版本：校验、写回版本号、更新 CHANGELOG、提交，并写入发布标记 `.quailegg/release.json`。
2. 发布：构建资产（如有）、发布到 registry、打 tag、推送、创建 GitHub Release。

`qegg plan` 会告诉你当前处于哪个阶段，以及接下来要执行的步骤。

## pr 模式（默认）

```text
合并功能 PR ──► qegg release ──► 发布 PR（版本 + CHANGELOG + 标记）
                                    │
                               人工评审、合并
                                    ▼
                              qegg release ──► publish / tag / push / Release
```

第一次运行只完成"提升版本"，结果以 PR 的形式推到 `[release].branch`（默认 `release`）。新的记录合并进来时，同一个 PR 会被更新。发布 PR 合并后，主分支上出现发布标记，下一次运行进入"发布"阶段。

适合需要人工把关版本号和 changelog 的项目。

## commit 模式

```toml
[release]
mode = "commit"
```

一次运行完成全部步骤：校验、提升版本、更新 CHANGELOG、提交、发布、打 tag、推送、创建 Release。没有人工评审环节，qegg 会对此给出提醒。

## 步骤与安全检查

| 步骤 | 说明 |
| --- | --- |
| preflight | 检查分支、工作区是否干净、包与清单版本是否与计划一致 |
| verify | 执行 `[verify].commands` |
| bump | 写回各包清单的版本号 |
| changelog | 更新 `CHANGELOG.md` 并消费变更记录 |
| commit | 按 `commit_message` 提交 |
| assets | 构建发布资产（仅在启用时） |
| publish | 发布到 registry，版本已存在则跳过 |
| tag | 创建 tag，已存在且指向 HEAD 则跳过 |
| push | 推送分支与 tag |
| github-release | 创建 GitHub Release，已存在则跳过 |

资产在 publish 之前构建，构建失败就不会发布或打 tag。

## 本地调试发布

以下参数可以组合，用来在本地演练：

```bash
qegg release --dry-run                                   # 只打印计划
qegg release --no-publish --no-push --no-github-release  # 只在本地提交与打 tag
qegg release --skip-verify --allow-dirty                 # 跳过校验、允许脏工作区
```

## 中断与续跑

每次运行的状态保存在 `.quailegg/state/`（不进版本库）。网络抖动、凭证过期导致发布中断后：

```bash
qegg resume
```

qegg 会读取最近一次运行，跳过已完成的步骤继续执行。由于 publish、tag、Release 都会先检查目标是否已存在，重复执行是安全的。

`qegg status` 可以查看当前版本、tag 与 registry 上的发布情况，`--offline` 跳过 registry 查询。
