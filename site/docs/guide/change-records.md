# 变更记录

变更记录是 QuailEgg 的输入。每个用户可见的改动都在 `.changes/` 下新增一个 Markdown 文件。

## 一条记录长什么样

```markdown title=".changes/cache-expiry.md"
---
type: feat
scope: cache
packages:
  my-package: minor
---

缓存支持配置过期时间。
```

front matter 决定版本怎么变，正文会原样进入 CHANGELOG。

- `type` 是约定式提交的类型，例如 `feat`、`fix`；
- `packages` 把受影响的包映射到 `patch`、`minor` 或 `major`；
- `scope` 可选，用来在 changelog 里标注影响范围；
- `release: false` 的记录只进 changelog，不触发版本提升。

全部字段见 [变更记录格式](../reference/change-record)。

## 版本怎么计算

同一个包被多条记录引用时，取其中最高的级别。比如两条 `patch` 加一条 `minor`，这个包升一个 minor 版本。

```bash
qegg plan
```

会列出每个包的当前版本、下一个版本以及参与计算的记录。

## 用 qegg commit 生成

手写也可以，但更推荐 `qegg commit`：

```bash
qegg commit                                   # 交互式
qegg commit -m "fix: handle empty input" \
  --packages my-package:patch                 # 非交互
qegg commit --no-commit ...                   # 只写记录，不提交
```

`-m` 接收约定式提交信息，qegg 会从中拆出 `type`、`scope` 与说明，同时用它作为 git 提交信息。

## pre-commit hook

`qegg init` 安装的 hook 在每次提交前做两件事：

1. 如果在**项目仓库的默认分支**上提交、且暂存区改动了 `[changes].scope` 范围内的文件，要求至少有一条合法的变更记录（hook 比较 `origin` 与 `[project] repo`，fork 克隆里不拦）；
2. 当暂存区包含 `[changes].scope` 范围内的文件时运行 `[verify].commands`，失败则拒绝提交；scope 之外的提交跳过校验。

只改文档、CI 配置等 scope 之外的文件不需要记录，也不会触发校验命令。`scope` 为空表示整个仓库。普通分支和 fork 克隆上的提交不会被拦，合并前的 `qegg check --ci` 会按 PR 差异要求一条能正常解析的记录。

::: warning
确实需要跳过 hook 时，用 `git commit --no-verify`。CI 中的 `qegg check --ci` 仍会按 PR 差异检查记录是否缺失。
:::

## 写作建议

- 一句话能说清就不要写两句；
- 描述用户能感知到的变化，而不是实现细节；
- 文件名用小写字母、数字和短横线，例如 `cache-expiry.md`。
