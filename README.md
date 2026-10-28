# QuailEgg

QuailEgg 是一个自动发版工具。代码合并到主分支后，QuailEgg 汇总 `.changes/` 中的变更记录，计算版本号，更新 `CHANGELOG.md`，然后发布软件包并创建 Git Tag 与 GitHub Release。

## 特性

- 每次改动必须附带 `.changes/` 变更记录，由 git hook 强制检查，生效目录可配置
- 根据模板汇总 `.changes/`，生成 `CHANGELOG.md`
- 按 SemVer 规则计算版本号，MAJOR 升级可配置人工审批
- 自动识别项目类型，支持 npm、PyPI、crates.io
- 发布步骤记录状态，失败后可从中断处恢复，不会重复发布
- `qegg init` 同时生成 GitHub Actions 工作流，合并到主分支即自动发版

## 使用

```bash
qegg init        # 生成配置、git hook 和 GitHub Actions 工作流
qegg commit      # 生成变更记录并提交
qegg check       # 检查环境与配置
qegg plan        # 预览发版计划，不修改任何文件
qegg changelog   # 根据 .changes/ 生成 CHANGELOG.md
qegg release     # 执行发布
qegg resume      # 恢复中断的发布
qegg status      # 查看发布状态
```

## 变更记录

每次修改都需要在 `.changes/` 下新增一个 Markdown 文件，一个文件对应一条记录。文件名用一到两个单词概括本次修改内容，例如 `.changes/cache-expiry.md`：

```markdown
---
type: feat
scope: cache
packages:
  cache: minor
  server: patch
---

新增缓存过期策略配置，支持自定义缓存有效期。
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | 是 | `feat`、`fix`、`perf`、`docs`、`refactor` 等，决定 Changelog 分组 |
| `scope` | 否 | 影响范围，如 `cache` |
| `packages` | 是 | 参与本次改动的包，以及各自的升级类型 `patch`、`minor`、`major` |
| `release` | 否 | 是否参与发版，默认 `true`；`false` 时只记入 Changelog |

`packages` 写成映射，一个包一行，对应 TUI 里为每个包选的升级类型。单包项目只写一项，例如 `cache: minor`。升级类型为 `major` 即表示不兼容变更，不再单设 `breaking` 字段。


`qegg commit` 会生成变更记录并提交，省去手写元数据。无论带不带 `-m` 都会进入交互界面：

```bash
qegg commit
qegg commit -m "feat(cache): 支持自定义缓存有效期"
```

交互流程：

1. 确认变更说明，带 `-m` 时预填提交信息
2. 选择需要升版本的包，依赖了改动包的其他包会被自动列出并默认勾选
3. 为每个包选择升级类型（patch / minor / major），编写具体修改，如：“新增缓存过期策略配置，支持自定义缓存有效期”
4. 确认后生成记录并选择是否提交

例如只改了包 1，包 2 依赖包 1，界面会同时勾选包 1 和包 2，由你决定包 2 是否跟着升版本。

```toml
[changes]
dir = ".changes"
# hook 生效的目录，留空表示全仓库
scope = ["src", "crates"]
```

## 配置

在项目根目录创建 `.quailegg.toml`：

```toml
schema_version = 1

[project]
default_branch = "main"

[git]
tag_prefix = "v"

[publish]
registries = ["npm"]
```

未填写的配置项使用自动检测结果或默认值。发布凭证通过环境变量或 CI Secrets 提供，不要写入配置文件。

## 项目规范

- [语义化版本](https://semver.org/lang/zh-CN/)
- [约定式提交](https://www.conventionalcommits.org/zh-hans/v1.0.0/)

## License

[MIT](LICENSE)
