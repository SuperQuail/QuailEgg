# 发布资产

发布时可以把构建产物（二进制、安装包等）连同校验和上传到 GitHub Release。

## 两种来源

- Go 包：qegg 调用生态自带的交叉编译；
- 其他项目（包括 Tauri）：在 `[release.assets].commands` 里写构建命令，用 `files` 收集产物。

## 启用

发布资产默认关闭。写入任意字段或 `enabled = true` 即启用：

```toml
[release.assets]
enabled = true
```

## Go 默认行为

- 目标平台：`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64`
- 构建参数：`CGO_ENABLED=0`、`-trimpath`、`-buildvcs=false`
- 版本注入：`-ldflags -X main.version={{version}}`
- 产物：`<name>_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`）与 `checksums.txt`
- 输出目录：`.quailegg/assets/<tag>/`，不进版本库

`cmd/` 下有多个 main 包时，自动模式会告警并禁用资产，需要显式设置 `main`。库项目不会构建。

## 自定义构建

```toml
[release.assets]
enabled = true
commands = [
  "pnpm -C ui install --frozen-lockfile",
  "pnpm -C ui build",
  "cargo tauri build",
]
files = [
  "target/release/bundle/msi/*.msi",
  "target/release/bundle/nsis/*.exe",
]
```

- `commands` 在仓库根目录执行，`{{version}}` / `{{tag}}` 会被替换，环境变量里有 `QEGG_VERSION`、`QEGG_TAG`；
- `files` 是仓库相对 glob，`**` 可跨目录；匹配到的文件复制到 `.quailegg/assets/<tag>/`，同名文件直接报错；
- 自动生成 `checksums.txt`，上传时排在最后；
- 写入 `files` 后不再走 Go 交叉编译；`commands` 必须和 `files` 一起使用。

## 多平台构建

Tauri 这类项目需要各平台自己构建（macOS 的 dmg 只能在 macOS 上打）。用 `[[release.build]]` 让生成的工作流按平台跑：

```toml
[[release.build]]
name = "windows"
runner = "windows-latest"
steps = ["pnpm -C ui install --frozen-lockfile", "pnpm -C ui build", "cargo tauri build"]
artifacts = ["target/release/bundle/msi/*.msi"]

[[release.build]]
name = "macos"
runner = "macos-latest"
steps = ["pnpm -C ui install --frozen-lockfile", "pnpm -C ui build", "cargo tauri build"]
artifacts = ["target/release/bundle/dmg/*.dmg"]
```

生成的 release job 会下载全部 artifact 到 `.quailegg/assets/downloaded`，然后执行：

```bash
qegg release --yes --assets-dir .quailegg/assets/downloaded
```

手写工作流时也可以用同一个参数，把任何已构建好的目录交给 qegg 上传。

需要 `uses:` 自定义 action 时，把 `steps` 换成 `yaml`（两者互斥），见 [GitHub Actions](github-actions#自定义-actionyaml)。只上传文件：目录（例如 macOS 的 `.app`）下载后会被拆平，应该上传 dmg / msi / deb / AppImage。

多包仓库需要 `version_scope = "shared"`：一个 GitHub Release 只能挂一个版本的资产；有 `[[release.build]]` 时也不能把 `[release.assets].enabled` 设为 `false`。

## 跳过与上传

- `qegg release --no-assets` 跳过构建与上传；
- `github_release = false` 时只构建不上传；
- 上传依赖 `gh`，使用 `--clobber`，重跑会覆盖同名资产。
