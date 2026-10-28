# 命令行

```bash
qegg <command> [flags]
```

## 全局参数

| 参数 | 说明 |
| --- | --- |
| `--config <path>` | `.quailegg.toml` 的路径 |
| `--cwd <dir>` | 工作目录，默认当前目录 |
| `--verbose` | 输出详细日志 |
| `--no-color` | 禁用颜色 |
| `--lang <en\|zh>` | 界面语言 |

## qegg init

生成配置、变更记录目录、git hook 与 GitHub Actions 工作流。

| 参数 | 说明 |
| --- | --- |
| `--no-input` | 不进入交互界面 |
| `--dry-run` | 只打印结果，不写文件 |
| `--json` | 输出 JSON |
| `--force` | 覆盖已存在的文件 |
| `--mode <pr\|commit>` | 发布方式 |
| `--branch <name>` | 主分支名 |
| `--tag-prefix <prefix>` | Tag 前缀，默认 `v` |
| `--remote <name>` | git remote，默认 `origin` |
| `--registries <list>` | 发布目标，例如 `npm,pypi,go` |
| `--changes-dir <dir>` | 变更记录目录，默认 `.changes` |
| `--scope <dirs>` | hook 生效的目录 |
| `--verify-cmd <cmd>` | 发版前校验命令，可重复 |
| `--no-verify-cmd` | 不写入校验命令 |
| `--no-hook` | 不生成 git hook |
| `--no-workflows` | 不生成 GitHub Actions 工作流 |
| `--no-status-workflow` | 不生成 status 工作流 |
| `--no-add-packages` | 不自动登记新发现的包 |
| `--qegg-version <v>` | 工作流固定使用的 qegg 版本，默认取当前运行版本 |

## qegg commit

生成一条变更记录并提交。

| 参数 | 说明 |
| --- | --- |
| `-m, --message <msg>` | 约定式提交信息 |
| `--type <type>` | 变更类型，例如 `feat` |
| `--scope <scope>` | 影响范围 |
| `--body <text>` | 变更说明 |
| `--packages <list>` | 包与升级级别，例如 `my-pkg:minor` |
| `--no-commit` | 只写记录，不提交 |
| `--no-input` | 不进入交互界面 |
| `-y, --yes` | 跳过确认 |

## qegg check

检查配置、生态、工具与变更记录。

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出 JSON |
| `--ci` | 按 PR 差异检查是否缺少变更记录 |

## qegg plan

预览发版计划，不修改任何文件。

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出 JSON |
| `--markdown` | 输出 Markdown，适合贴到 PR 评论 |

## qegg verify

执行 `[verify].commands`。

## qegg changelog

根据待发变更生成 `CHANGELOG.md`。

| 参数 | 说明 |
| --- | --- |
| `--dry-run` | 只打印结果，不写文件 |

## qegg release

提升版本并发布。

| 参数 | 说明 |
| --- | --- |
| `--dry-run` | 只打印计划 |
| `-y, --yes` | 跳过确认 |
| `--allow-dirty` | 允许工作区不干净 |
| `--skip-verify` | 跳过发版前校验 |
| `--no-push` | 不推送 |
| `--no-publish` | 不发布到 registry |
| `--no-github-release` | 不创建 GitHub Release |
| `--no-assets` | 不构建也不上传发布资产 |

## qegg resume

继续上一次中断的发布。

| 参数 | 说明 |
| --- | --- |
| `-y, --yes` | 跳过确认 |

## qegg status

查看发布状态。

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出 JSON |
| `--offline` | 跳过 registry 查询 |

## qegg lang

```bash
qegg lang          # 查看当前语言与支持的语言
qegg lang zh       # 设置并保存到用户配置
```

## qegg version

打印 qegg 版本。
