# 变更记录格式

位置：`[changes].dir`（默认 `.changes/`）下的 `*.md` 文件，`README.md` 除外。

```markdown
---
type: feat
scope: cache
packages:
  my-package: minor
release: true
---

缓存支持配置过期时间。
```

## 字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | 是 | `feat`、`fix`、`perf`、`docs`、`refactor`、`test`、`build`、`ci`、`chore`、`style`、`revert` |
| `scope` | 否 | 影响范围 |
| `packages` | 是 | 已登记的包名到 `patch`、`minor`、`major` 的映射 |
| `release` | 否 | 默认 `true`，为 `false` 时只进 changelog |

## 正文

front matter 之后的内容就是 changelog 条目。建议一到四句英文或中文短句，描述用户能感知的变化。

## 文件名

小写字母、数字和短横线，例如 `cache-expiry.md`，最多三个短横线分隔的词。文件名只用于区分记录，不影响版本计算。
