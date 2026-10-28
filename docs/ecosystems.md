# 生态扩展指南

## 已支持的生态

| 生态 | 清单 | Registry | 版本写回 | 发布命令 |
| --- | --- | --- | --- | --- |
| Node.js | `package.json` | npm | `version` 字段 | `npm publish` |
| Python | `pyproject.toml` | PyPI | `[project].version`、`[tool.poetry].version`、hatch/setuptools 动态版本文件 | `uv publish` / `twine upload` |
| Rust | `Cargo.toml` | crates.io | `[package].version` | `cargo publish` |
| Go | `go.mod` | proxy.golang.org | 无（版本来自 git tag） | 无（push tag 即发布） |

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

   版本来自 tag 的生态（目前是 Go）额外实现可选接口 `ecosystem.TagVersioner`：`CurrentVersion` 从 git tag 推导当前版本，`CheckVersion` 校验下一个版本（Go 的 v2+ 需要 `/vN` module 路径）。清单里有版本的生态不需要实现它。

   `Spec()` 返回 `ecosystem.Spec{ID, Language, Manifests, Publishable, BuildRequired, WorkflowSetup, WorkflowEnv, Tools}`；`WorkflowSetup` 与 `WorkflowEnv` 分别用包内的 `workflow-setup.yml`（YAML 序列项）和 `workflow-env.yml`（YAML 映射）通过 `//go:embed` 提供，均从第 0 列开始；`Publisher()` 返回本生态的发布实现，并实现 `Publisher` 的 `Name/Precheck/Build/Publish`，不需要构建的生态 `Build` 返回 nil。

3. 在 `internal/ecosystem/registry/registry.go` 的 `All()` 里加一行，例如 `npm.New()`。

4. 在 `internal/ecosystem/testdata/` 加一份 fixture，并在子包内写测试；用 `internal/ecosystem/ecosystemtest` 的 `Fixture` 复制 fixture，避免测试改写仓库里的文件。

5. 更新本文件的生态表格与 `docs/architecture.md` 的目录树；README 的功能列表按需同步。

## Go 的边界

- 只支持仓库根目录的 `go.mod`。子目录模块需要 `sub/vX.Y.Z` 形式的 tag，属于多包发布，v0.1 不支持；`go.work` 多模块仓库同理。
- Go 模块版本就是 tag，因此 `git.tag_prefix` 必须是 `v`（配置成其他前缀会得到警告，且版本推导会退化到 0.0.0）。
- v2 及以上的 tag 要求 module 路径带 `/vN` 后缀。qegg 在 `plan` 阶段直接报错并给出期望路径，不会自动改写 `go.mod` 与 import。
- 发布步骤为空操作：push tag 之后 Go 模块代理会按需抓取。预检查询 proxy.golang.org 只反映“是否已被缓存”，私有模块或尚未被抓取的版本返回 404，属正常现象。
- 首次发布没有历史 tag 时以 `0.0.0` 为基线：`feat` 记录得到 0.1.0，`patch` 记录得到 0.0.1。

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
