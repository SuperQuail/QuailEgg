# 发布资产

Go 项目可以在发布时交叉编译二进制，打包后连同校验和上传到 GitHub Release。

## 启用

发布资产默认关闭。写入任意字段或 `enabled = true` 即启用：

```toml
[release.assets]
enabled = true
```

## 默认行为

- 目标平台：`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64`
- 构建参数：`CGO_ENABLED=0`、`-trimpath`、`-buildvcs=false`
- 版本注入：`-ldflags -X main.version={{version}}`
- 产物：`<name>_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`）与 `checksums.txt`
- 输出目录：`.quailegg/assets/<tag>/`，不进版本库

## 自定义

```toml
[release.assets]
name = "mytool"
main = "cmd/mytool"
targets = ["linux/amd64", "darwin/arm64"]
ldflags = ["-s", "-w", "-X main.version={{version}}"]
```

`ldflags` 只支持 `{{version}}` 与 `{{tag}}` 两个占位符。

`cmd/` 下有多个 main 包时，自动模式会告警并禁用资产，需要显式设置 `main`。库项目不会构建。

## 跳过与上传

- `qegg release --no-assets` 跳过构建与上传；
- `github_release = false` 时只构建不上传；
- 上传依赖 `gh`，使用 `--clobber`，重跑会覆盖同名资产。
