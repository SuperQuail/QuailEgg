# 多包仓库

一个仓库可以包含多个包。QuailEgg 为每个包登记清单位置，再根据记录决定哪些包需要发布、采用什么版本，以及按什么顺序发布。

本页使用两个 npm 包作为例子：

```text
packages/
├── core/     # @acme/core
└── cli/      # @acme/cli
```

## 登记包

`qegg init` 可以识别 npm/pnpm、uv 和 Cargo workspace，并额外扫描根目录下最多三层目录内的包清单。`qegg scan` 可以在初始化前查看候选和不可登记的原因。已登记的包保存在配置中：

```toml
[packages."npm@acme/core"]
name = "@acme/core"
path = "packages/core"
resolver = "npm"

[packages."npm@acme/cli"]
name = "@acme/cli"
path = "packages/cli"
resolver = "npm"
depends-on = ["npm@acme/core"]
```

表名是包的身份键：新配置使用 `<resolver>@<清单名>`，并用 `name` 记录真实清单名；旧式清单名键仍然有效。路径相对于仓库根目录。`depends-on` 可以补充包之间的依赖关系（写身份键），发布时先处理被依赖的包。

增加 workspace 成员后，可以再次运行 `qegg init` 补充登记，再检查配置差异。默认会登记全部可登记的候选；只想登记其中一部分时用 `qegg init --packages` 指定（值取自 `qegg scan` 的 `id`）。超过三层且没有 workspace 声明的包仍需显式写入配置；`qegg plan` 和 `qegg check` 会按配置中的路径读取这些包。

## 选择版本策略

| 策略 | 适用情况 | 发布结果 |
| --- | --- | --- |
| `package` | 各包有各自的发布节奏 | 根据每个包的记录分别计算版本和标签 |
| `shared` | 多个包组成同一个产品或保持同步发布 | 所有包共用一个版本和标签 |

配置默认值是 `package`。首次初始化时，如果检测到多个包的版本相同，`qegg init` 会选择 `shared`，因此请检查实际生成的配置。

### 独立版本

```toml
[release]
version_scope = "package"
```

假设 `@acme/core` 当前是 `1.2.3`，`@acme/cli` 当前是 `2.0.0`。记录可以分别指定升级级别：

```yaml
packages:
  "@acme/core": minor
  "@acme/cli": patch
```

下一个版本分别是 `1.3.0` 和 `2.0.1`。没有升级记录的包不会仅因为另一个包发布而自动获得新版本。

独立多包的标签默认使用 `{{slug}}-v{{version}}`，也可以通过 `[git].tag_format` 调整。

### 共享版本

```toml
[release]
version_scope = "shared"
```

所有登记包的当前版本必须一致。QuailEgg 取记录中的最高升级级别，为所有包计算同一个新版本，并共用一个标签，例如 `v1.3.0`。

对于共用根版本的 Cargo 成员，`shared` 还可以避免多个包分别改写同一个版本字段。具体限制见[多语言生态](./ecosystems#rust)。

## 指定记录影响哪些包

一个逻辑改动可以涉及多个包，并为各包选择不同的升级级别：

```bash
git add -A
qegg commit --no-input -m "feat: support configurable expiration" --body "Allow users to configure cache expiration." --packages npm@acme/core:minor,npm@acme/cli:patch
```

也可以把包拆成两个参数：

```bash
qegg commit --no-input -m "feat: support configurable expiration" --body "Allow users to configure cache expiration." --packages npm@acme/core:minor --packages npm@acme/cli:patch
```

在独立版本模式下，如果调用方也需要发布新版本，请在记录中明确列出它。声明 `depends-on` 用于安排发布顺序，不等于自动为依赖方创建升级记录。

提交前用 `qegg plan` 检查各包的当前版本、目标版本和发布顺序。

## 只更新版本，不发布软件包

仓库可能包含内部工具或应用前端，它们需要跟随版本变化，但不需要发布到 registry：

```toml
[packages."npm@internal-tools"]
name = "internal-tools"
path = "tools"
resolver = "npm"
publish = false
```

`publish = false` 只跳过软件包发布，不会把这个包排除在版本管理之外。它仍按独立或共享策略更新版本。

## 跨生态共用版本

Tauri 应用常见的结构是 Rust 后端与前端共用一个应用版本。例如：

```toml
[release]
version_scope = "shared"

[packages."rust@app-core"]
name = "app-core"
path = "src-tauri"
resolver = "rust"
publish = false

[packages."npm@app-ui"]
name = "app-ui"
path = "ui"
resolver = "npm"
publish = false

[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
```

这里假设两个清单中的包名分别是 `app-core` 和 `app-ui`，并且它们与 JSON 文件的版本已经一致。前端位于嵌套目录、又不属于已识别的 workspace 时，需要显式登记。若 npm 清单与 Rust crate 同名（例如都叫 `app`），可以登记为 `npm@app` 和 `rust@app` 两个包，并使用共享版本。

`qegg init` 会在适用时自动登记可读取的 Tauri 版本文件。你可以用 `qegg check` 检查版本是否一致。

跨生态共享版本还有两个限制：

- 各生态必须用相同的标签拼写。Python 的预发布写法与其他生态不同，因此不能混合共享预发布标签。
- 当前 Go 适配器仅支持仓库根模块，不能加入多包发布计划。

多平台安装包的构建与上传见[发布资产](./assets)。

## 检查配置结果

```bash
qegg check
qegg plan
```

除了版本，还应确认包名、清单路径、发布开关和依赖顺序。配置字段的完整定义见[配置项参考](../reference/config)。
