# GitHub Actions

`qegg init` 默认生成三个工作流，都安装与生成时相同版本的 qegg。

| 工作流 | 触发 | 作用 |
| --- | --- | --- |
| `qegg-pr.yml` | 向主分支发起 PR（release 分支除外） | `qegg check --ci` 检查记录是否缺失，并把发布计划评论到 PR |
| `qegg-release.yml` | 推送到主分支（仅发布相关路径变化时）、手动触发 | `qegg release --yes` |
| `qegg-status.yml` | 手动触发 | `qegg status --json` |

## 触发路径

release 工作流只在下列路径变化时运行：`.changes/**`、`.quailegg.toml`、`.quailegg/pre-release.lock`、`.quailegg/release.json`、`CHANGELOG.md`，以及各生态清单（`package.json`、`pyproject.toml`、`Cargo.toml`、`go.mod`）。只改文档的推送不会重建发布 PR。

## 发布 PR

- pr 模式每次推送主分支都会重建 `release` 分支并强推，同一个发布 PR 被原地刷新，多个功能合并共用一个版本；
- 发布 PR 由 `github-actions[bot]` 用 `GITHUB_TOKEN` 创建，GitHub 不会为它正常启动检查：PR 上可能出现 `action_required`，或一个 0 job 的启动失败 run。`qegg-pr` 会跳过 release 分支（`if: github.head_ref != 'release'`），不要把这条检查设为必需；验证由发布阶段那次 run 负责。

不需要 status 工作流时用 `--no-status-workflow`，完全不生成工作流用 `--no-workflows`。

## 自定义发布工作流

生成的 `qegg-release.yml` 默认在 `ubuntu-latest` 上跑一个 release job。`[release.workflow]` 可以换 runner、超时和 release job 的前置步骤：

```toml
[release.workflow]
runner = "ubuntu-latest"
timeout_minutes = 60
steps = ["pnpm -C ui install --frozen-lockfile"]
```

`[[release.build]]` 增加按平台的构建 job（见 [发布资产](assets)）：每个 job 跑 checkout、setup-go、各生态 setup 和你的 `steps`，再把 `artifacts` 上传；release job 下载全部产物后执行 `qegg release --yes --assets-dir .quailegg/assets/downloaded`。

```toml
[[release.build]]
name = "windows"
runner = "windows-latest"
steps = ["pnpm -C ui build", "cargo tauri build"]
artifacts = ["target/release/bundle/msi/*.msi"]
```

### 自定义 action（`yaml`）

需要 `uses:`（`pnpm/action-setup`、固定 rust 版本、`tauri-action` 等）时，用 `yaml` 代替 `steps`，两者互斥。`yaml` 模式完全接管 steps：qegg 不再自动加 checkout / setup-go / 生态 setup，只保留 artifact 上传，所以 `checkout` 和工具链要自己写。

```toml
[[release.build]]
name = "macos-arm"
runner = "macos-latest"
timeout_minutes = 60
yaml = """
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }
- uses: pnpm/action-setup@v4
  with: { version: 11 }
- uses: actions/setup-node@v4
  with: { node-version: 22, cache: pnpm, cache-dependency-path: ui/pnpm-lock.yaml }
- uses: dtolnay/rust-toolchain@master
  with: { toolchain: "1.94.0", targets: aarch64-apple-darwin }
- run: pnpm --dir ui install --frozen-lockfile
- run: pnpm --dir ui build
- run: pnpm --dir ui tauri build --target aarch64-apple-darwin
"""
artifacts = ["target/release/bundle/**/*"]
```

shell `steps` 需要环境变量时用 `env`（`yaml` 模式请在块内写 `env:`）：

```toml
[[release.build]]
name = "windows"
runner = "windows-latest"
env = { TAURI_SIGNING_PRIVATE_KEY = "${{ secrets.TAURI_KEY }}" }
steps = ["pnpm -C ui tauri build"]
artifacts = ["target/release/bundle/msi/*.msi"]
```

`tauri-action` 默认会自己创建 GitHub Release 并上传安装包，会和 qegg 的 tag + Release 步骤重复。managed 模式建议只用 `tauri build` 构建、产物交给 qegg；要让 `tauri-action` 自己发 Release，就用下面的 `external` 模式。

## 外部工作流

不想让 qegg 生成发布工作流时：

```toml
[release.workflow]
mode = "external"
```

`qegg init` 只生成 PR 与 status 工作流，`qegg-release.yml` 由仓库自己维护；发版时调用 `qegg release --yes`（多平台产物用 `--assets-dir` 交给 qegg 上传）。

## 生态与凭证

工作流只包含项目实际使用的生态：优先看 `[publish].registries`，其次看各包的 `resolver`。只发 npm 的项目不会出现 Python 的步骤与凭证。

在仓库 Settings → Secrets 中添加对应 Secret：

| 生态 | Secret |
| --- | --- |
| npm | `NPM_TOKEN` |
| Python | `UV_PUBLISH_TOKEN` |
| Rust | `CARGO_REGISTRY_TOKEN` |
| Go | 无需凭证 |

`GITHUB_TOKEN` 由 Actions 自动提供。

## 仓库设置

- pr 模式需要在 Settings → Actions → General 中允许 GitHub Actions 创建 PR；
- 分支保护规则需要允许 `GITHUB_TOKEN` 推送到主分支，否则发布会卡在 push 步骤；
- 发布工作流跳过 `github-actions[bot]` 自己的推送，避免循环触发。

## 固定 qegg 版本

生成的工作流固定使用某个已发布版本，例如 `go install ...@v0.2.0`。升级时重新生成：

```bash
qegg init --force --qegg-version v0.3.0
```

版本落后是安全的，只是工具链滞后。
