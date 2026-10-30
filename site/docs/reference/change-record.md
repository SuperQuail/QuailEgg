# 变更记录格式

记录位于 `[changes].dir` 指定的目录，默认为 `.changes/`。QuailEgg 读取其中的 `*.md`，跳过 `README.md`。

本页定义格式和字段；写作与提交过程见[变更记录](../guide/change-records)。

## 完整示例

```markdown title=".changes/cache-expiry.md"
---
type: feat
scope: cache
packages:
  my-package: minor
release: true
---

Allow users to configure cache expiration.
```

使用 YAML front matter 描述元数据，结束分隔线之后是记录正文。

## 字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | 是 | 改动类型，使用下方支持的值 |
| `scope` | 否 | 改动涉及的功能范围 |
| `packages` | 是 | 已登记包名到升级级别的非空映射 |
| `release` | 否 | 默认 `true`；为 `false` 时不参与版本提升 |

### type

支持 `feat`、`fix`、`perf`、`refactor`、`docs`、`test`、`build`、`ci`、`chore`、`style` 和 `revert`。

类型描述改动意图，不会自动决定版本升级级别。

### packages

包名必须与配置中登记的名称一致。一个记录可以包含多个包，并分别指定 `patch`、`minor` 或 `major`：

```yaml
packages:
  "@acme/core": minor
  "@acme/cli": patch
```

同一个包的多条记录取最高升级级别。共享版本模式会为所有登记包选择同一个版本，见[多包仓库](../guide/monorepos)。

使用命令生成时，等价输入为 `--packages @acme/core:minor,@acme/cli:patch`，也可以重复传入 `--packages`。完整命令见[多包输入](./cli#多包输入)。

### release

`release: false` 的记录不推动新版本，但可在相关包的后续发布中进入更新日志。`packages` 仍然必填。

因此，只新增这类记录不会创建一个新版本；独立多包仓库中，其他包发布也不代表这条记录一定会被收录。

## 正文

正文必填，会原样进入更新日志。可以使用英文或中文，建议用简短、完整的句子描述用户能感知的变化。

项目可以另外约定正文长度、语言和写作风格；这些编辑规范与格式校验分别生效。

`qegg commit --body` 指定这段正文。省略时，正文取自提交标题去掉约定式前缀后的描述，通常需要再检查是否适合作为发布说明。

## 文件名

使用小写字母、数字和短横线，以 `.md` 结尾，最多三个短横线分隔的词，例如：

```text
cache-expiry.md
empty-input-fix.md
```

文件名只用于区分记录，不参与版本计算。同名文件不能覆盖；使用 `qegg commit` 时会自动生成并检查文件名。

## 记录的生命周期

稳定版发布准备阶段会将相关记录写入更新日志，再删除已收录的记录。预发布保留记录，并在退出渠道后的稳定版统一处理。

`.changes/README.md` 由初始化生成，不是变更记录。
