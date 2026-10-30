# 生态扩展指南

## 已支持的生态

| 生态 | 清单 | Registry | 版本写回 | 发布命令 |
| --- | --- | --- | --- | --- |
| Node.js | `package.json` | npm | `version` 字段 | `npm publish` |
| Python | `pyproject.toml` | PyPI | `[project].version`、`[tool.poetry].version`、hatch/setuptools 动态版本文件 | `uv publish` / `twine upload` |
| Rust | `Cargo.toml` | crates.io | `[package].version`；继承时写根 `[workspace.package].version` | `cargo publish` |
| Go | `go.mod` | proxy.golang.org | 无（版本来自 git tag） | 无（push tag 即发布），默认附带交叉编译的二进制资产 |

## 工作流里注入什么，由项目决定

`qegg init` 只为项目实际使用的生态生成 setup 步骤和凭证：优先取 `[publish].registries`（`npm`、`pypi`、`crates` 会映射到 `npm`、`python`、`rust`；`go` 映射到 `go`），否则取 `[packages.*].resolver`，最后才回退到检测到的第一个生态。因此只做 npm 的项目不会出现 `UV_PUBLISH_TOKEN`，同时发布到多个 registry 的项目会得到全部对应的步骤与凭证。

| 生态 | 环境变量 | Secret |
| --- | --- | --- |
| npm | `NODE_AUTH_TOKEN` | `NPM_TOKEN` |
| Python | `UV_PUBLISH_TOKEN` | `UV_PUBLISH_TOKEN` |
| Rust | `CARGO_REGISTRY_TOKEN` | `CARGO_REGISTRY_TOKEN` |
| Go | —（模块代理公开，无凭证） | — |

## 为什么不是硬编码

每个生态用一个 `Spec` 描述自己（`Manifests`、`Tools`、`WorkflowSetup` 等），定义在 [internal/ecosystem/ecosystem.go](../internal/ecosystem/ecosystem.go)，由 [internal/ecosystem/registry/registry.go](../internal/ecosystem/registry/registry.go) 汇总：

- `qegg init` 检测不到包时会列出 `registry.ManifestListText()` 的内容，新增语言后自动带上新清单；
- `qegg check` 的必需工具来自 `Spec.Tools`；
- `qegg init` 生成的 Actions 工作流里，语言相关的 setup 步骤来自 `Spec.WorkflowSetup`，发布凭证环境变量来自 `Spec.WorkflowEnv`；两者分别是包内的 `workflow-setup.yml` 与 `workflow-env.yml`（第 0 列起，由 qegg 缩进），不是 Go 字符串。

因此“支持一种新语言”不需要改动散落在各处的提示文案。

## 新增一种语言的步骤

1. 新建子包 `internal/ecosystem/<id>/`，按职责拆成 `spec.go`、`detect.go`、`version.go`、`publish.go`，并导出 `func New() ecosystem.Ecosystem`。

2. 实现 `ecosystem.Ecosystem` 的四个方法：

   ```go
   type Ecosystem interface {
       Spec() Spec
       Detect(root string) ([]Package, error)
       SetVersion(root string, pkg Package, next string) error
       Publisher() Publisher
   }
   ```

   `Spec()` 是唯一的静态能力声明：

   | 字段 | 作用 |
   | --- | --- |
   | `ID` / `Language` / `Manifests` | 标识、展示名、检测清单 |
   | `PublishTargets` | 发布目标（`npm` / `pypi` / `crates` / `go`）；`qegg init` 用它推断 `[publish].registries` 和工作流凭证 |
   | `TagPrefix` | 版本来自 tag 时要求的前缀（Go 为 `v`）；`plan` 用它告警，空表示不限 |
   | `WorkflowSetup` / `WorkflowEnv` | 生成的 Actions 工作流里注入的 setup 步骤与凭证，分别来自包内 `workflow-setup.yml` / `workflow-env.yml`（第 0 列起，`//go:embed`） |
   | `Tools` | `qegg check` 需要的可执行文件，提示文案在 `tool.<name>.*` |

   按需实现可选接口，核心代码只通过 registry 查询，不按生态名分支：

   - `TagVersioner`：版本来自 git tag 时实现 `CurrentVersion` / `CheckVersion`（Go 的 v2+ 需要 `/vN` module 路径）。清单里有版本的生态不需要实现。
   - `InitAdvisor`：`DefaultVerifyCommands(root)`，`qegg init` 写进 `[verify].commands` 的默认命令。混合生态仓库会按 resolver 排序去重收集。
   - `VersionSyncer`：同步兄弟依赖 pin 与 lockfile（Cargo workspace、uv workspace）。
   - `VersionStyle`：版本拼写与 tag 拼写不同时实现（Python 的 PEP 440）。
   - `VersionFileAdvisor`：清单之外还有文件承载版本时实现（Tauri 的 `tauri.conf.json`）。返回的只是候选路径，`qegg init` 会先确认文件里是字面量版本才写进 `[[version.file]]`。
   - `AssetPlanner`：`PlanAssets` 规划生态自己构建的发布资产（Go 交叉编译 main 包）。返回 nil 表示这个包没有可构建的资产；没有实现该接口的生态改用 `[release.assets].commands` + `files`。
   - `Publisher`：`Name/Precheck/Build/Publish`；不需要构建的生态 `Build` 返回 nil。

   Go 的差异全部落在这张表里：版本来自 tag → `TagVersioner`；tag 必须 `v` → `Spec.TagPrefix`；清单没有版本 → `SetVersion` 空实现；发布即 push tag → `Publisher` 空实现；资产由 Go 交叉编译 → `AssetPlanner`；多包共享版本由现有的“多包 + tag-versioned”通用规则拒绝。核心代码没有 `if resolver == "go"`。

   `config.Validate` 只做 schema 校验（含 `[[version.file]]` 的路径与 scope 组合）；未知 resolver/registry、shared 与 tag-versioned 冲突等跨生态规则由 `qegg init` / `plan` / `check` 通过 registry 判断。

3. 在 `internal/ecosystem/registry/registry.go` 的 `All()` 里加一行，例如 `npm.New()`；需要的能力查询（`PublishTargetsFor`、`EcosystemForTarget`、`DefaultVerifyCommands`、`VersionSyncerFor`、`AssetPlannerFor`、`VersionFileCandidates` 等）已经在 registry 里。

4. 在 `internal/ecosystem/testdata/` 加一份 fixture，并在子包内写测试；用 `internal/ecosystem/ecosystemtest` 的 `Fixture` 复制 fixture，避免测试改写仓库里的文件。

5. 更新本文件的生态表格与 `docs/architecture.md` 的目录树；README 的功能列表按需同步。核心代码里不要出现 `config.Resolver*` 或生态名字面量，`internal/ecosystem/registry/policy_test.go` 会检查；确需临时例外要写进 allowlist 并注明原因。

## 发现与解析

qegg 把“包”分成两个阶段：

- **发现**（`qegg init`）：`Detect` 只读根清单和根声明的 workspace，用来建议初始 `[packages.*]`；
- **解析**（每次 plan/check/status/release）：不看发现结果，直接遍历配置里的包，按 `path` 调适配器的 `Inspect` 读取清单。

因此没有被 workspace 声明、也不会被发现的独立子工程（例如 Tauri 的 `ui/`），只要手写一条包配置就能参与版本、变更记录和 scope：

~~~toml
[packages."sc2translater-ui"]
path = "ui"
resolver = "npm"
publish = false
~~~

- 配置 key 必须和清单里的 name 一致，否则报错；
- 路径必须是仓库相对路径、不能越界；清单缺失会报“manifest 不存在”，而不是“未检测到”；
- 未登记但被发现的包仍然只是警告。

清单之外承载版本的文件（Tauri 的 `src-tauri/tauri.conf.json`）用 `[[version.file]]` 登记，版本阶段会把它与清单一起改写，并并入 release commit：

~~~toml
[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
~~~

`qegg init` 会向适配器询问候选（rust 适配器认识 `tauri.conf.json` 与 `src-tauri/tauri.conf.json`），确认文件里是字面量版本后自动写入配置。`qegg check` 会比对文件里的版本与包版本，漂移直接报错。

各适配器都实现了 `Inspect`：npm 读 `package.json`、rust 读 `Cargo.toml`（继承版本回读根 workspace）、python 读 `pyproject.toml`、go 只接受根目录的 `go.mod`。

## Go 的边界

- 只支持仓库根目录的 `go.mod`。子目录模块需要 `sub/vX.Y.Z` 形式的 tag，属于多包发布，v0.1 不支持；`go.work` 多模块仓库同理。
- Go 模块版本就是 tag，因此 `git.tag_prefix` 必须是 `v`（配置成其他前缀会得到警告，且版本推导会退化到 0.0.0）。
- v2 及以上的 tag 要求 module 路径带 `/vN` 后缀。qegg 在 `plan` 阶段直接报错并给出期望路径，不会自动改写 `go.mod` 与 import。
- 发布步骤为空操作：push tag 之后 Go 模块代理会按需抓取。预检查询 proxy.golang.org 只反映“是否已被缓存”，且使用带 `v` 的规范版本号；私有模块或尚未被抓取的版本返回 404，属正常现象。
- 首次发布没有历史 tag 时以 `0.0.0` 为基线：`feat` 记录得到 0.1.0，`patch` 记录得到 0.0.1。

## 多包仓库

`qegg init` 会把同一仓库里的所有包登记进 `[packages.*]`。发布有两种版本范围：

- `version_scope = "package"`（默认）：每个包按自己的变更记录独立 bump、独立 tag、独立 GitHub Release。tag 由 `[git] tag_format` 决定，默认 `{{slug}}-v{{version}}`（如 `core-v1.2.3`）；变更日志按 `<!-- qegg:release core@1.2.3 -->` 分段，标题为 `## core v1.2.3 - 日期`。
- `version_scope = "shared"`：所有包共用一个版本、一个 tag（沿用 `tag_prefix`），一次发布全部包；没有记录的包也同步版本。要求所有包当前版本一致，但可以跨生态（Tauri 的 Rust crates 与前端共用应用版本）；tag 拼写必须一致，因此带 PEP 440 拼写的 Python 包在预发布时不能与其他生态共享版本。

发布顺序按清单里声明的依赖和 `depends-on` 排序：被依赖的包先发布。只有命中本次发布包的记录才会被消费，`release: false` 记录留到对应包发布时再写入变更日志。Go 模块不支持多包仓库；`[release.assets]` 与 `[[release.build]]` 要求单包仓库或 `shared` scope（一个 GitHub Release 只挂一个版本）。

Python 的预发布沿用仓库里的拼写（`2.0.2-a14`），tag 与 PyPI 使用 PEP 440 形式（`2.0.2a14`），由适配器转换。

`[[version.file]]` 要求整仓只有一个发布版本，因此只能在单包仓库或 `version_scope = "shared"` 下使用；多包 `package` scope 会在加载配置时直接报错。

## Cargo workspace

Rust 适配器支持 Cargo workspace：根清单是虚拟清单（只有 `[workspace]`）也可以，成员的 `version.workspace = true` 会解析成根清单 `[workspace.package].version`。Cargo 一个 workspace 只能有一个版本，所以：

- 继承版本的 workspace 必须用 `version_scope = "shared"`。`package` scope 下有两个及以上继承成员时，`qegg plan` 给出警告，`qegg release` 直接报错，而不是写出一个自相矛盾的仓库。
- bump 时只改根 `[workspace.package].version`；如果根清单同时是包（有 `[package]`），它自己的版本照常写回。
- 兄弟 `path` 依赖的 `version` 跟着更新，缺 `version` 的内联表会补上（`[workspace.dependencies]` 与成员清单都处理），否则 `cargo publish` 会拒绝。
- 有 `Cargo.lock` 时执行 `cargo update --workspace` 并把它并入 release commit；`Cargo.lock` 被 `.gitignore` 忽略时只刷新、不提交。
- `[package]` 里的 `publish = false`，或 `publish = ["..."]` 且不含 `crates-io`，都会被当作不发布；`qegg init` 会把它写进 `[packages.*].publish`。
- 一个仓库只支持根目录的一个 workspace，嵌套 workspace 不识别。

## 发布资产

发布资产在 publish 之前准备，构建失败就不会发布或打 tag。默认关闭，需要在 `[release.assets]` 里写任意字段或 `enabled = true` 才会启用。来源有三种：

1. **生态自己构建**：Go 通过 `AssetPlanner` 规划 main 包矩阵，`internal/release/assets` 负责交叉编译、打包与校验和。默认目标 linux/amd64、linux/arm64、darwin/amd64、darwin/arm64、windows/amd64；产物是 `<name>_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`）与 `checksums.txt`，输出到 `.quailegg/assets/<tag>/`，不进版本库。多个 main 包时自动模式告警并禁用，需要 `[release.assets].main` 指定；库项目不构建。
2. **自定义构建**：`commands` 在仓库根目录执行（支持 `{{version}}` / `{{tag}}` 占位符，注入 `QEGG_VERSION`、`QEGG_TAG`），`files` 按仓库相对 glob（`**` 可用）收集产物。写入 `files` 后不再走生态构建；`commands` 必须与 `files` 一起使用。同名产物直接报错。
3. **CI 预构建**：`qegg release --assets-dir <dir>` 直接收集目录里的文件，不再本地构建。managed 工作流的 `[[release.build]]` 各平台 job 用它把安装包交给 release job 上传；只收文件，目录（例如 macOS 的 `.app`）不会保持结构，应该上传 dmg / msi / deb / AppImage。

- `--no-assets` 跳过；GitHub Release 关闭时只构建不上传
- 上传依赖 `gh`，`--clobber` 保证重跑幂等

## 发布工作流

`qegg init` 生成 `qegg-release.yml`、`qegg-pr.yml`、`qegg-status.yml`。release 工作流的内容由 `[release.workflow]` 和 `[[release.build]]` 决定：

- `[release.workflow]`：`mode`（`managed` / `external`）、`runner`、`timeout_minutes`、`steps`（release job 的前置 shell 步骤）或 `yaml`（原样 steps 块，与 `steps` 互斥）、`env`
- `[[release.build]]`：每平台一个构建 job，`name` / `runner` / `timeout_minutes` / `steps` 或 `yaml` / `env` / `artifacts`；shell 模式下 job 固定跑 checkout、setup-go、各生态 setup，再跑自定义步骤；`yaml` 模式完全接管 steps（仍需自己写 checkout），qegg 只补 `qegg-assets-<name>` 上传
- release job `needs` 全部构建 job，下载 artifact 到 `.quailegg/assets/downloaded`，执行 `qegg release --yes --assets-dir ...`
- `mode = "external"` 时不生成 `qegg-release.yml`，只生成 PR 与 status 工作流；发布工作流由仓库自己维护，仍需调用 `qegg release --yes`

| 配置 | 默认 |
| --- | --- |
| `enabled` | 关闭（写配置项或 `enabled = true` 开启） |
| `name` | main 包目录名 |
| `main` | 自动探测 `cmd/*` 或根目录 |
| `targets` | 上面的五个平台 |
| `ldflags` | `-X main.version={{version}}` |

## 后续计划

为了让 Java、Ruby、PHP、C#、Swift 这类生态无需每次都改 Go 代码，计划引入**声明式 resolver**：

```toml
[resolver.maven]
manifest = "pom.xml"
version-path = "project.version"
precheck = "https://repo1.maven.org/maven2/{{group}}/{{name}}/{{version}}/"
publish = ["mvn", "deploy"]
```

内置适配器继续负责“解析与写回”的复杂格式，声明式 resolver 负责“探测与发布命令”。两者共用同一套 `Spec` 与消息。