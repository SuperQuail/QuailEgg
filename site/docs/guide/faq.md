# 常见问题

## 提交被 hook 拒绝

暂存区改动了 `[changes].scope` 内的文件却没有变更记录。用 `qegg commit` 生成一条，或确认改动确实不需要发版后用 `git commit --no-verify`。

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
