# 变更记录

变更记录描述一次改动及其版本影响。你在开发时写下这些信息，发布时 QuailEgg 再将它们汇总成版本号和更新日志。

记录默认放在 `.changes/` 中。项目可以用 `[changes].scope` 指定哪些目录的改动需要记录。

## 一条记录长什么样

```markdown title=".changes/cache-expiry.md"
---
type: feat
scope: cache
packages:
  my-package: minor
---

Allow users to configure cache expiration.
```

上半部分是 front matter，用来描述这条记录；下半部分是用户会在更新日志中看到的说明。

- `type` 描述改动类型，例如新增功能用 `feat`，修复问题用 `fix`。
- `scope` 可选，用来标注改动涉及的功能范围。
- `packages` 指定受影响的包及其升级级别。包名应与配置中的登记名称一致。

`type` 和升级级别是分别填写的：将类型设为 `feat` 不会自动选择 `minor`。完整字段见[变更记录格式](../reference/change-record)。

## 选择升级级别

假设当前版本是 `1.2.3`：

| 级别 | 适用情况 | 下一个版本 |
| --- | --- | --- |
| `patch` | 向后兼容的问题修复 | `1.2.4` |
| `minor` | 向后兼容的新功能 | `1.3.0` |
| `major` | 需要用户调整用法的不兼容改动 | `2.0.0` |

同一个包有多条待发布记录时，QuailEgg 取最高级别，而不是逐条累加。例如，两条 `patch` 加一条 `minor`，会把 `1.2.3` 提升到 `1.3.0`。

你可以随时运行 `qegg plan` 查看实际结果。多包项目的版本选择见[多包仓库](./monorepos)，预发布编号见[预发布](./prereleases)。

## 用命令生成记录

先把与这次改动有关的代码加入暂存区：

```bash
git add -A
qegg commit
```

`qegg commit` 会写入记录，暂存这个记录文件，并提交当前暂存区。因此，运行它之前应先检查 `git diff --cached`，确认暂存的内容属于同一个逻辑改动。

非交互用法如下：

```bash
qegg commit --no-input -m "fix(cache): handle empty input" --body "Return an empty result for empty input." --packages my-package:patch
```

这里有两段不同的说明：

- `-m` 是 Git 提交标题，QuailEgg 也会从中提取类型和范围。
- `--body` 是变更记录正文，会原样进入更新日志；省略时，使用提交标题去掉类型与范围后的描述。

如果只想生成记录，稍后自行提交，可以加 `--no-commit`：

```bash
qegg commit --no-input --no-commit -m "fix(cache): handle empty input" --body "Return an empty result for empty input." --packages my-package:patch
```

这个命令只写文件。之后需要自行暂存记录，并与对应代码一起提交。

## 一条记录影响多个包

同一个逻辑改动涉及多个包时，可以用逗号分隔包名与升级级别：

```bash
qegg commit --no-input -m "feat: support configurable expiration" --body "Allow users to configure cache expiration." --packages @acme/core:minor,@acme/cli:patch
```

也可以重复传入 `--packages`，结果相同：

```bash
qegg commit --no-input -m "feat: support configurable expiration" --body "Allow users to configure cache expiration." --packages @acme/core:minor --packages @acme/cli:patch
```

这会生成一条记录，分别为 `@acme/core` 指定 `minor`、为 `@acme/cli` 指定 `patch`。示例假设两个包都已登记；提交前同样需要先暂存对应代码。

独立版本与共享版本如何处理这些级别，见[多包仓库](./monorepos)。

## 不提升版本的说明

有些变化值得写进更新日志，却不需要单独提升版本。这时可以手写记录，并设置：

```yaml
release: false
```

`packages` 仍然必填，但其中的升级级别不会参与版本计算。这类说明会随相关包的后续发布一起写入更新日志；只有这类记录时，不会因此产生一个新版本。

## 提交前会检查什么

初始化安装的 pre-commit hook 包含两项检查，它们的生效条件不同：

| 检查 | 生效条件 | 失败后的行为 |
| --- | --- | --- |
| 变更记录检查 | 在项目克隆的默认分支提交，且暂存文件命中 `[changes].scope` | 缺少有效记录时拒绝提交 |
| 校验命令 | 暂存文件命中 `[changes].scope`，且配置了 `[verify].commands` | 命令失败时拒绝提交 |

普通分支和 fork 克隆跳过的是**变更记录强制检查**，不是校验命令。功能 PR 合并前，`qegg check --ci` 会根据整个 PR 的差异检查记录。

hook 通过配置的 Git remote（默认为 `origin`）与 `[project].repo` 比较仓库身份。`scope` 为空表示覆盖整个仓库；如果文档也在范围内，文档改动同样可能触发检查。变更记录和工具管理的文件有单独的排除规则。

::: tip 查看实际检查结果
运行 `qegg hook pre-commit` 可以执行与 hook 相同的检查。若只是想单独运行校验命令，使用 `qegg verify`。
:::

## 写出有用的发布说明

记录正文会原样展示给用户。与其写“调整缓存判断逻辑”，更有帮助的写法是“空输入现在返回空结果”。

建议每条记录围绕一个变化，交代用户能做什么，或者哪个问题得到修复。文件名保持简短，使用小写字母、数字和短横线，例如 `cache-expiry.md`，最多三个单词。

稳定版发布时，QuailEgg 会删除已经写入更新日志的记录；预发布会保留它们，直到稳定版发布。你不需要手动清理。
