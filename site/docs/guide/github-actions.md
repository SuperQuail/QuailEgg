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
