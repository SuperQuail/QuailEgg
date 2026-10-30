# 命令行参考

```bash
qegg <command> [flags]
qegg <command> --help
```

本页用于查询命令与参数。首次使用请从[快速上手](../guide/quick-start)开始，发布阶段的含义见[发布流程](../guide/release-flow)。

## 全局参数

| 参数 | 说明 |
| --- | --- |
| `--config <path>` | 指定配置文件，默认查找 `.quailegg.toml` |
| `--cwd <dir>` | 指定工作目录，默认当前目录 |
| `--verbose` | 输出详细日志 |
| `--no-color` | 禁用颜色 |
| `--lang <en\|zh>` | 指定本次运行的界面语言 |

## qegg init

生成配置、变更记录说明、Git hook 和 GitHub Actions 工作流，并更新 `.gitignore` 中的托管区块。

仓库必须配置远程，默认使用 `origin`。已有工作流和 hook 默认保留；`--force` 允许重新生成它们。已有配置会保留并按需补充包、仓库身份与版本文件。

| 参数 | 说明 |
| --- | --- |
| `--no-input` | 跳过交互向导 |
| `--dry-run` | 预览生成结果，不写文件 |
| `--json` | 输出 JSON |
| `--force` | 重新生成已有的托管文件 |
| `--mode <pr\|commit>` | 发布方式，首次初始化默认 `pr` |
| `--branch <name>` | 主分支名 |
| `--tag-prefix <prefix>` | 标签前缀，默认 `v` |
| `--remote <name>` | Git remote，默认 `origin` |
| `--registries <list>` | 发布目标，逗号分隔，例如 `npm,pypi` |
| `--changes-dir <dir>` | 记录目录，默认 `.changes` |
| `--environment <name>` | release job 使用的 GitHub Actions environment |
| `--scope <dirs>` | 记录检查和提交前校验覆盖的目录，逗号分隔 |
| `--verify-cmd <cmd>` | 校验命令，可重复传入 |
| `--no-verify-cmd` | 不为初始化结果选择校验命令 |
| `--no-hook` | 不生成本地 hook |
| `--no-workflows` | 不生成工作流 |
| `--no-status-workflow` | 不生成状态查询工作流 |
| `--no-add-packages` | 不自动登记新发现的包 |
| `--qegg-version <version>` | 工作流使用的 QuailEgg 版本，接受已发布版本或 `latest` |

```bash
qegg init --no-input --mode pr --registries npm
qegg init --dry-run --force
```

## qegg commit

写入一条记录，暂存该记录，再提交当前暂存区。运行前需要自行暂存代码；`--no-commit` 只写记录，不暂存或提交。

| 参数 | 说明 |
| --- | --- |
| `-m, --message <text>` | Git 提交标题，可从约定式标题提取类型与范围 |
| `--type <type>` | 显式指定记录类型 |
| `--scope <scope>` | 显式指定记录范围 |
| `--body <text>` | 进入更新日志的记录正文 |
| `--packages <list>` | 包名与升级级别，支持逗号分隔和重复传入 |
| `--no-commit` | 只生成记录 |
| `--no-input` | 跳过交互向导，需要提供包与升级级别 |
| `-y, --yes` | 确认选项；脚本调用仍应显式提供 `--no-input` 和所需字段 |

### 单包输入

```bash
git add -A
qegg commit --no-input -m "fix(cache): handle empty input" --body "Return an empty result for empty input." --packages my-package:patch
```

### 多包输入

一个参数可以为多个包指定不同的升级级别：

```bash
qegg commit --no-input -m "feat: support configurable expiration" --body "Allow users to configure cache expiration." --packages @acme/core:minor,@acme/cli:patch
```

也可以重复传入参数，下面与上例等价：

```bash
qegg commit --no-input -m "feat: support configurable expiration" --body "Allow users to configure cache expiration." --packages @acme/core:minor --packages @acme/cli:patch
```

包名取自配置中的 `[packages]`，级别为 `patch`、`minor` 或 `major`。这些例子都只生成一条记录；`packages` 中会包含多个包。版本策略见[多包仓库](../guide/monorepos)。

## qegg check

检查配置、登记包、所需工具和记录。它会报告校验命令配置，但不会代替 `qegg verify` 执行这些命令。

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出 JSON |
| `--ci` | 额外按 PR 差异检查是否缺少记录 |

## qegg plan

根据当前状态预览版本和步骤，不修改文件。

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出 JSON |
| `--markdown` | 输出适合 PR 评论的 Markdown |

## qegg verify

执行 `[verify].commands`，遵循配置的超时和 `fail_fast`。即使当前没有暂存文件，也可以手动运行。

## qegg hook

```bash
qegg hook pre-commit
```

执行与安装的 hook 相同的检查。记录强制规则针对项目克隆的默认分支；校验命令则按暂存文件是否命中 scope 决定，不受这个分支条件限制。

详见[提交前检查](../guide/change-records#提交前会检查什么)。

## qegg changelog

根据当前计划生成 `CHANGELOG.md`。这个命令会写文件，不等同于预览发布计划。

| 参数 | 说明 |
| --- | --- |
| `--dry-run` | 输出生成结果，不写文件 |

正常发布由 `qegg release` 管理更新日志；只想查看计划时，使用 `qegg plan --markdown`。

## qegg pre

进入、退出或查看预发布渠道：

| 用法 | 说明 |
| --- | --- |
| `qegg pre` | 查看当前渠道与下一个版本 |
| `qegg pre alpha` | 进入 alpha 渠道 |
| `qegg pre beta` | 进入 beta 渠道 |
| `qegg pre rc` | 进入 rc 渠道 |
| `qegg pre exit` | 退出渠道 |

进入渠道会写入 `.quailegg/pre-release.lock`，退出时删除。新增、修改和删除都需要提交，CI 才会使用新状态。切换渠道前必须先退出当前渠道。

具体版本计算和稳定版操作见[预发布](../guide/prereleases)。

## qegg release

根据当前状态准备版本或执行发布。PR 模式的首次运行以创建发布 PR 为目标，不能把每次运行都理解为立即上传软件包。

| 参数 | 说明 |
| --- | --- |
| `--dry-run` | 只展示计划，不执行 |
| `-y, --yes` | 跳过发布确认 |
| `--allow-dirty` | 放宽版本准备阶段的工作区检查 |
| `--skip-verify` | 跳过 `[verify].commands` |
| `--no-push` | 跳过发布分支或版本提交、标签的推送 |
| `--no-publish` | 跳过软件包 registry 发布 |
| `--no-github-release` | 跳过创建 GitHub Release 和资产上传 |
| `--no-assets` | 跳过资产构建和上传 |
| `--assets-dir <dir>` | 收集预构建产物，替代本地资产构建 |

`--no-push` 不会自动禁止软件包发布；本地演练的组合方式见[发布流程](../guide/release-flow#在本地预览和演练)。

## qegg resume

展示最近一次本地运行的信息，再根据当前状态重新规划并执行发布。它不会自动恢复上一次调用的参数。

支持 `-y, --yes`，以及 `release` 的执行参数：`--allow-dirty`、`--skip-verify`、`--no-push`、`--no-publish`、`--no-github-release`、`--no-assets`、`--assets-dir`。不支持 `--dry-run`。

```bash
qegg resume --assets-dir ./dist
```

## qegg status

查看本地版本、标签、待发布记录、最近一次本地运行，以及 registry 状态。

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出 JSON |
| `--offline` | 跳过 registry 查询 |

离线或查询失败时，registry 状态可能为未知。Go 模块还需区分标签是否存在与代理是否缓存版本，见[问题排查](../guide/faq#status-显示未发布或未知)。

## qegg lang

```bash
qegg lang
qegg lang zh
```

不带参数时显示语言信息；带 `en` 或 `zh` 时保存到当前用户的配置。优先级为 `--lang`、`QEGG_LANG`、用户配置，默认英文。

## qegg version

打印当前运行的 QuailEgg 版本。
