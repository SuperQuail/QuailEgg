# 常见问题

## init 提示没有远程仓库

`qegg init` 需要仓库已经配置远程，因为仓库身份（`[project] repo`）要写进配置，hook 靠它识别 fork 克隆：

```bash
git remote add origin https://github.com/you/project
qegg init
```

## fork 里提交不会被 hook 拦

设计如此。记录规则只在项目仓库的默认分支上强制；fork 克隆和普通分支的提交放行，合并前的 PR 检查会要求一条能正常解析的记录。

## 发布 PR 上的检查是红的或一直等待批准

发布 PR 由 `github-actions[bot]` 用 `GITHUB_TOKEN` 创建，GitHub 不会为这类 PR 正常启动检查，run 可能停在 `action_required` 或直接启动失败。`qegg-pr` 会跳过 release 分支；不要把这条检查设为必需。

## 只改了文档，为什么没有触发发布

release 工作流只在发布相关路径变化时运行（`.changes/**`、配置文件、marker、`CHANGELOG.md` 与生态清单）。只改文档的推送会被跳过，这是预期行为。

## 怎么发 alpha / beta

```bash
qegg pre alpha     # 写入 .quailegg/pre-release.lock
git add .quailegg/pre-release.lock
git commit -m "chore: start alpha"
git push
qegg pre exit      # 毕业：下一次发布回到稳定版
```

## 提交被 hook 拒绝

两种原因：

1. 暂存区改动了 `[changes].scope` 内的文件却没有变更记录：用 `qegg commit` 生成一条，或确认改动确实不需要发版后用 `git commit --no-verify`；
2. 暂存区有 scope 内的改动，且 `[verify].commands` 执行失败：先修复命令再提交。scope 之外的提交（文档、site、CI）不会运行校验命令。

如果提示找不到 `qegg`，确认 `$(go env GOPATH)/bin` 在 `PATH` 中。

## 提示工作区不干净

`qegg release` 要求工作区干净。先提交或暂存改动，或者加 `--allow-dirty`。

## 提示只能在某分支发版

发版只能在 `[project].default_branch` 上执行。切到主分支后再运行。

## 清单版本与计划不一致

计划生成后有人手动改了 `package.json` 等清单里的版本。重新运行 `qegg plan`。

## CI 无法创建发布 PR

在 Settings → Actions → General 中勾选允许 GitHub Actions 创建和批准 PR，或在本地以 pr 模式运行 `qegg release`。

## 发布卡在 push

分支保护不允许 `GITHUB_TOKEN` 推送。调整保护规则，或在本地运行 `qegg release --yes`。

## Go 大版本升级报错

v2 及以上需要 `/vN` module 路径。先修改 `go.mod` 与 import，再发版。

## `qegg status` 显示未发布

Go 模块代理在 push tag 后首次 `go get` 时才会缓存版本，这与 tag 是否存在无关。

## 切换界面语言

```bash
qegg lang zh      # 保存到用户配置
qegg --lang en plan
QEGG_LANG=zh qegg plan
```

优先级依次是 `--lang`、`QEGG_LANG`、用户配置，默认英文。
