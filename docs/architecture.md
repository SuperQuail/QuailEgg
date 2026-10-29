# 架构与目录约定

## 目录

```
cmd/qegg/                  # 进程入口，只调用 cli.Execute
internal/
  cli/                     # 根命令、全局 flag、Runtime（Options/Runtime/ExitError），每个子命令一个文件，向导在 wizard.go
  app/                     # 用例编排：init/plan/release/check/commit/status/...，报告类型与用例同文件
  release/                 # 发布计划、执行器、版本阶段、发布阶段、marker 生成、changelog 片段、指纹、GitOps 接口
  release/state/           # .quailegg/release.json 与运行记录（.quailegg/state）的持久化
  verify/                  # 发版前校验命令执行
  ecosystem/               # 仅抽象：Package/Tool/Spec/Status、Ecosystem 与 Publisher 接口、CheckURL、ExpandGlobs/Exclude
  ecosystem/registry/      # 唯一注册入口：All/ByID/Specs/SpecFor/DetectAll/DetectFor/SetVersion/PublisherFor/Precheck/Build/Publish
  ecosystem/npm/           # Node.js 适配器（package.json、npm/pnpm 多包仓库）
  ecosystem/python/        # Python 适配器（PEP 621、Poetry、hatch/setuptools 动态版本、uv 多包仓库）
  ecosystem/rust/          # Rust 适配器（Cargo.toml、cargo 多包仓库）
  ecosystem/golang/        # Go 适配器（go.mod，版本来自 git tag，push tag 即发布）
  ecosystem/ecosystemtest/ # 测试共用的 fixture 复制工具
  ecosystem/testdata/      # 各生态的 fixture
  scaffold/                # init 的计划、应用、配置渲染、产物清单
    assets/                # 生成的 git hook 与 GitHub Actions 工作流模板（.tmpl，[[ ]] 动作）
  changes/                 # .changes 记录解析与写入，以及 hook/CI 规则（MatchesScope/IsWithin/Enforce）
  changelog/               # CHANGELOG.md 渲染与更新
  git/                     # git CLI 封装
  github/                  # GitHub Release / PR API、贡献者 login 解析
  platform/
    execx/                 # 可注入的命令执行器
    fsx/                   # 原子写、带预期校验的编辑
    ui/                    # 输出打印
  i18n/
    locales/               # en.toml / zh.toml
  config/ semver/ tui/
test/e2e/                  # 端到端测试
```

## 依赖方向

```
cmd/qegg ──► cli ──► app ──► release ──► {release/state, verify, ecosystem/registry, github}
                     │        └► {changes, changelog, config, semver, platform}
                     ├► scaffold ──► ecosystem/registry
                     └► {git, changes, changelog, verify}

ecosystem/registry ──► ecosystem ◄── {npm, python, rust, golang}
platform/* 与 i18n 为叶子；不得反向依赖。
```

cli 直接引用 release、scaffold、changelog、semver、tui、config 与 ecosystem/registry（打印计划、init 摘要、向导）；config、semver、platform、i18n 是多个上层共用的下层。

注册表单独成包（`ecosystem/registry`）是为了避免子包与 registry 循环导入：各生态子包只依赖 `ecosystem` 定义接口，由 registry 反向汇总子包。

## SOLID 落点

- **SRP**：每个子命令一个文件；发布计划、执行器、版本阶段、发布阶段、marker、本地状态各自一个文件；TUI 每个向导一个文件。
- **OCP**：新增生态只需实现 `ecosystem.Ecosystem`（`Spec/Detect/SetVersion/Publisher`）并在 `registry.All()` 里加一行；版本来自 git tag 的生态（Go）再实现可选的 `ecosystem.TagVersioner`（`CurrentVersion/CheckVersion`）。工具检查与工作流模板都由 `Spec` 驱动，新工具只需补 `tool.<name>.*` 文案。
- **LSP**：三个 `Publisher` 适配器遵守同一套 Precheck/Build/Publish 语义（不需要构建的生态 Build 返回 nil）。
- **ISP**：`release.GitOps` 只包含执行器真正用到的 11 个方法；`execx.Runner` 只有 Run。
- **DIP**：`release.Deps` 依赖 `GitOps` 接口而非具体 `*git.Repo`；`cli.Runtime` 按需构造 `*ui.Printer` 与 `*app.App`，子命令不自行创建依赖。

## 生成的资产与模板

`qegg init` 写出的 git hook 与三个 Actions 工作流不再内嵌在 Go 字符串里：

- 正文放在 `internal/scaffold/assets/`，目录结构镜像产物布局；`.tmpl` 后缀表示需要变量替换，无后缀表示原样拷贝；
- `assets.go` 用 `//go:embed assets` 打包，`render.go` 用 `text/template` 渲染，动作分隔符是 `[[` / `]]`，因此产物里的 `${{ secrets.X }}` 保持原样；需要字面量 `[[` 时写 `[[ "[[" ]]`；
- `artifacts.go` 的 `artifacts` 清单声明「源文件 → 目标路径、权限、启用条件、init 输出的 reason」，`Build` 按清单顺序生成计划；
- 生态相关的 setup 步骤与发布凭证放在各自包的 `workflow-setup.yml`、`workflow-env.yml`（第 0 列起），由 `Spec.WorkflowSetup`、`Spec.WorkflowEnv` 暴露，模板分别用 `indent 6` 与 `indent 10` 缩进进 steps 列表和 release 步骤的 `env`；只注入项目实际使用的生态（`[publish].registries` 优先，其次包的 resolver）。

新增一个生成文件：放资产 + 在清单里加一条；新增一个生态：在自己的包放 `workflow-setup.yml` 并 embed。两者都不再需要写字符串拼接。Go 是例外：基础工作流为了编译 qegg 已经安装 Go，因此它的 setup 与 env 片段为空，`qegg init` 不会追加额外步骤。

## 文件规模

- 单个 Go 文件以 1000 行为上限；超过上限或职责混杂时拆分。
- 一个包可以只有一个文件（`semver`、`platform/*`、`git`、`github`、`verify`），前提是职责单一。
- 用户可见文案只能出现在 `internal/i18n/locales`；有测试守护。
