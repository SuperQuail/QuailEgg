# 自举与首发

qegg 自己用 qegg 发布。这个仓库的版本号来自 git tag（Go 生态），所以"第一个版本怎么来"和"发布时用哪个 qegg"需要按下面的顺序走。

## 版本号规则

- Go module 没有版本字段，版本就是 **可达 HEAD 的最高 `v*` tag**；没有 tag 时视为 `0.0.0`，因此首个 `feat` 记录得到 `v0.1.0`，首个 `patch` 记录得到 `v0.0.1`。
- 生成的 workflow 安装 `@latest`（`go install ...@latest`），每次 CI 都用最新的已发布版本。发布流程只依赖已发布版本，**不会、也不能引用正在发布的版本**，所以不存在版本号自指或循环。
- `@latest` 表示不可复现：新版本发布后 CI 自动用上它，新版本有缺陷时 CI 也会跟着坏；需要固定时执行 `qegg init --force --qegg-version vX.Y.Z`，固定检查、发布和状态工作流使用的 QuailEgg 版本。

## 阶段 A：本地全流程演练（无远端）

```bash
go test ./...                      # 单元测试 + e2e

# 克隆一份仓库，用 bare 仓库当 origin
git clone . /tmp/qegg-rehearsal && cd /tmp/qegg-rehearsal
git init --bare /tmp/qegg-origin.git
git remote set-url origin /tmp/qegg-origin.git
git push origin HEAD

qegg release --yes --no-publish --no-github-release
git tag                            # v0.1.0
git --git-dir=/tmp/qegg-origin.git tag
qegg plan                          # 没有待发布内容
```

e2e 里的 `TestGoReleaseAgainstLocalRemote` 固化了同样的流程（连续两次发版、本地与远端 tag、CHANGELOG、marker），CI 上离线运行。

## 阶段 B：生成自举脚手架

```bash
go install ./cmd/qegg              # 让 pre-commit hook 能找到 qegg
qegg init --no-input --mode commit --registries go
qegg plan                          # 0.0.0 -> 0.1.0
git add -A
qegg commit -m "feat: add Go module support" \
  --packages "github.com/SuperQuail/QuailEgg:minor"
```

`qegg init` 要求仓库已经配置好远程（默认 `origin`）：仓库身份会写进 `[project] repo`，本地钩子靠它区分“项目仓库”和“fork 克隆”。没有远程时 init 直接失败，并提示 `git remote add`。

钩子的变更记录规则只在**项目仓库的默认分支**上生效：fork 克隆、普通分支上的提交都不会被拦，合并前的 PR 检查会要求一条记录。

如果 `qegg` 不在 PATH，hook 会拒绝提交：先 `qegg commit --no-commit` 写记录，再 `git commit --no-verify`。

## 阶段 C：第一个真实版本

```bash
# 本地发版：commit 模式会直接提交版本、打 tag、推送并创建 GitHub Release
qegg release --yes --no-publish          # gh 未登录时加 --no-github-release，事后手动 gh release create
go install github.com/SuperQuail/QuailEgg/cmd/qegg@v0.1.0
```

第一次本地发布前不要先单独 push 主分支：如果 workflow 已存在，它可能抢先发版。发布命令把分支和 tag 一起推送，workflow 看到 marker 与 tag 后会空跑。

## 阶段 D：切换到 PR 模式

```bash
qegg init --no-input --force --mode pr --registries go
git add -A
qegg commit -m "ci: switch releases to pull requests" \
  --packages "github.com/SuperQuail/QuailEgg:patch"
git push origin main
```

CI 用最新的已发布 qegg 创建发布 PR；合并后由 publish 阶段打 tag、推送并创建 Release。

## 中断后的恢复

发布在半途失败（分支与 tag 都推上去了，却没能创建 GitHub Release，或某个附件没传完）时运行
`qegg resume`：它在正常的发布入口里先找到上一次运行记录中的那次发布，再只补还缺的部分。

- 远端同名 tag 必须存在，且指向本仓库同名 tag 的提交。远端明确没有这个 tag、或查询失败（无权访问、网络错误）时都只报告并停止，不写任何外部内容。
- 上一次运行的包上传全部完成或明确跳过、且这次发布启用了 GitHub Release 时，才补建 Release 或只补缺失的附件。原运行的发布说明、预发布属性和跳过选项原样沿用，不会被本次命令行参数改写。
- 运行记录里必须真的有一次“创建 GitHub Release 失败”：只跑到更早步骤、从未走到这一步的记录不算“只缺 Release”，`qegg resume` 只报告原因并停止。
- 已存在的同名附件按摘要核对：一致就跳过，摘要不同直接报冲突并停止，不会覆盖已有附件。
- 需要重新构建的附件会提示先按发布的源码提交重建，恢复流程本身不重新构建。

被中断的发布补建完成后即为完成状态：再次运行 `qegg resume` 只会提示这次发布已经完成，不会顺手开始下一版。
仓库里已有下一版变更记录时同样如此：`qegg resume` 只报告还有多少条记录在等，并提示运行 `qegg release`。
发布新版本一律显式运行 `qegg release`，CI 里由工作流触发。

只要 `.quailegg/release.json` 记录的发布已经打过 tag，`qegg resume` 就不会再规划新版本：
否则一个偶发的恢复会把本该属于下一次发布的变更记录当成新版本消费掉（升版本、写 CHANGELOG、提交发布并删除记录）。

创建与查询 GitHub Release 走 `gh`，只配置了 `GITHUB_TOKEN` 时走 REST；`GITHUB_API_URL` 指向非 github.com
（GitHub Enterprise 或本地测试服务器）时自动改用 REST。**上传附件目前只支持 `gh release upload`**：
即使 `GITHUB_API_URL` 指向别处，也需要 PATH 上有 `gh`。
## 发布执行记录（跨 runner 恢复）

`qegg release` 在第一次外部写操作之前，会把本次发布的执行记录写到 `[git].remote` 上一个独立引用里：

- 位置：`refs/qegg/ledger/<tag>`，里面的文件是 `.quailegg/publish-ledger.json`。它不是分支也不是 tag，普通的 clone、fetch、`git status` 都不会看到它；换 runner 之后用 `git ls-remote origin 'refs/qegg/ledger/*'` 就能确认它是否还在。生成的工作流已经有 `contents: write`，不需要额外权限。
- 内容：以仓库、发布提交、包、版本和目标源为键，记录每个包的结果（`not-started` / `started` / `uploaded` / `skipped` / `unknown`）与原因，以及资源构建、打 tag、推送、GitHub Release 等步骤的结果和整轮跳过决定（`--no-publish`、`--no-push`、`--no-github-release`、是否要创建 Release）。
- 何时写远端：只要本次发布可能产生外部写（上传、打 tag、建 Release），记录就写到远端；即使同时用了 `--no-push` 也一样——记录是恢复的依据，不是发布产物。整轮都不允许外部写时（例如 `--no-publish --no-push` 且关闭 Release），记录只留在本地 `.quailegg/state/ledger.json`，之后的普通 `qegg resume` 会读到它并沿用原来的决定。
- 互斥：新建记录是一次普通 push，引用已存在时会被拒绝；更新记录用 `git push --force-with-lease=<ref>:<上次读到的提交>` 做比较交换。两个并发的 `qegg resume` 只有一个能继续，另一个在下次外部写之前停下，提示 `release.err.ledger_conflict`。没有锁文件，崩溃不会留下锁。
- 上传之前先写 `started`，成功之后写结果；任何一次关键写入失败都会立即停止。恢复时只有"目标注册表确认版本不存在"或"记录里的首次发布尚未尝试"这两种情况才会上传，`started` 之后查询返回 404 不能当作重传依据。
- 旧记录：由 qegg v0.14 及更早版本写下的 `.quailegg/release.json`（schema 1）没有执行记录，恢复时按注册表回答判断——确认已存在就跳过、确认缺失就上传、查询失败或生态没有查询能力就停止并说明如何核实。
- 规划时就记录：schema 2 的发布在规划阶段（写 release commit 之前）就写下执行记录，因此"缺记录"通常意味着记录丢失。但当本次运行不得不自己创建记录时（例如规划在另一台机器上只写了本地副本），按"尚未尝试过"处理，好让没有查询能力的生态还能做首次 deploy。代价是：记录引用与发布标签都被删除时，这种发布与真正的首次发布无法区分，需要人工核对目标源——发布标签因此始终在上传之后才创建，也不会被自动删除。
- `qegg status` 会显示记录里的逐包结果与原因，`qegg status --offline` 只读本地副本（`.quailegg/state/ledger.json`，gitignore 内）。

## 常见失败

| 现象 | 处理 |
| --- | --- |
| workflow 里 `go install ...@vX` 401/404 | 仓库需公开；私有模块要配置 `GOPRIVATE` 与凭据（当前不支持） |
| `qegg status` 显示 published: no | 预检请求本身会触发 proxy 抓取；刚 push 的 tag 第一次可能仍是 no，重试一次即可。持续为 no 时检查模块路径大小写与 proxy 可达性 |
| 发布卡在 push | 分支保护不允许 GITHUB_TOKEN 推送；改用本地 `qegg release --yes` 或调整保护规则 |
| CI 无法创建发布 PR | 仓库设置里允许 GitHub Actions 创建 PR，或本地以 pr 模式发版 |
| 大版本升级报错 | v2+ 需要 `/vN` module 路径，先改 `go.mod` 与 import 再发版 |
| 无 tag 时版本是 0.0.0 | 正常；首个版本用 `feat` 记录即可得到 0.1.0 |
| publish 报找不到发布快照 | 浅克隆或缺历史：用 `fetch-depth: 0` 重新检出；本地可 `git fetch --unshallow` |
| publish 报 marker 未提交 | 版本已 bump 但提交没成功：把 `.quailegg/release.json` 随发布改动一起提交，或删除它重新发版 |
| publish 报发布源码与工作区不一致 | 工作区有未纳入发布的改动，或 `--assets-dir` 里的产物来自别的提交：清理工作区、补上 `qegg-assets.json` |

## 发布源码提交

publish 阶段会先固定本次发布所用的源码提交，再构建、上传和打 tag，因此主分支已经前移也不会发布到错误的树上：

- commit 模式用发布提交自身的 SHA；pr 模式在目标分支的第一父提交历史里按 marker 内容找实际合入的发布快照，squash 与 merge commit 都能定位。
- 主分支之后又有了新提交、甚至已经有下一版记录时，仍用原快照继续；本地 tag、远端 tag 都指向它。
- marker 必须先提交：只存在于工作区的 `.quailegg/release.json` 会让 publish 在第一次外部写操作前停止（版本已经 bump、提交却失败时就是这种状态）。
- 历史必须完整：浅克隆无法判断哪个提交引入了 marker，qegg 会直接报错并提示获取完整历史。生成的检查、发布和状态工作流都带 `fetch-depth: 0`。`mode = "external"` 的自维护工作流也要取全量历史。

## PR 模式

PR 模式下 release 分支是生成物：每次 push 到主分支，CI 都从主分支最新提交重建它，
提交版本号和 CHANGELOG 后 force push，并刷新同一个 release PR。功能 PR 合并只会让它变长，
主分支上的版本要等你合并 release PR 才更新；合并后新一轮 CI 打 tag、发布，
并把 release 分支拉平到主分支。

- release PR 用 squash 合并，标题固定为 `chore(release): bump versions`。
- 建议主分支开启 Require branches to be up to date before merging，避免过期的 release PR 被合入。
- release 工作流只在发布相关路径变化时运行（`.changes/**`、`.quailegg.toml`、`.quailegg/pre-release.lock`、`.quailegg/release.json`、`CHANGELOG.md` 与各生态清单；`[release].changelog = false` 时不含 `CHANGELOG.md`），只改文档的推送不会重建 release 分支。
- release PR 由 `github-actions[bot]` 用 GITHUB_TOKEN 创建，GitHub 不会为它正常启动检查：PR 上可能出现 `action_required`，或一个 0 job 的启动失败 run（合并/强推后尤其明显）。`qegg-pr` 的 check job 在 `if:` 里同时核对源仓库和源分支：只有本仓库发布分支（`[release].branch`，默认 `release`）的 PR 才跳过，fork 里的同名分支照常检查。run 一旦真正启动就会跳过这个 job，因此不要把这条检查设为必需；验证由 publish 那次 run 负责。需要真实检查只能换成 PAT/GitHub App 身份。
- workflow 安装 `@latest`，新版本发布后 CI 自动升级；要固定版本时用 `qegg init --force --qegg-version vX.Y.Z`。
- `[release] environment` 会渲染成发布 workflow 的 `environment:`，便于把 PyPI 凭证限制在受保护的 environment 里（NeoBot 这类项目用它做 `pypi` 审批）。
- CHANGELOG 的贡献者句柄取自 PR（没有 PR 时取 commit）作者的 GitHub login；私有仓库本地需要 `GITHUB_TOKEN`/`GH_TOKEN` 或登录过的 `gh`，都不可用时省略作者，不写显示名，也不影响发版。

## 开发模式（自举调试）

默认情况下，生成的工作流固定使用已发布的 qegg。修改 qegg 本身、又需要 CI 立刻用上当前源码时，可以打开开发模式：

```bash
qegg init --force --develop --no-input
```

这会写入顶层 `develop = true`，并调整检查、发布和状态工作流的安装步骤。

- 当前 checkout 是 QuailEgg 仓库（`go.mod` 的 module 为 `github.com/SuperQuail/QuailEgg` 且存在 `cmd/qegg`）时，直接 `go build ./cmd/qegg` 使用本地源码；
- 其他仓库退回 `go install github.com/SuperQuail/QuailEgg/cmd/qegg@main`。

开发模式不可复现，只用于自举调试：CI 会随当前分支变化，源码编译失败时发布也会失败。每个 release 的 marker 和本地 run record 会记录 `qegg.version`、`qegg.commit` 和 `qegg.develop`，便于确认某个版本由哪个构建产生。

稳定版发布前应关闭开发模式并固定版本：

```bash
qegg init --force --develop=false --qegg-version vX.Y.Z --no-input
```

检查生成的配置和工作流后，以 `ci: pin qegg to vX.Y.Z` 提交。`qegg init` 默认不会覆盖已有托管文件，因此修改 develop 后需要带 `--force`。

## 预发布（alpha / beta / rc）

预发布通过 `qegg pre` 和提交进仓库的锁文件表达，不需要改配置或给 CI 传参数：

```bash
qegg pre alpha      # 进入 alpha 渠道，写 .quailegg/pre-release.lock
git add .quailegg/pre-release.lock
git commit -m "chore: start alpha releases"
git push            # CI 发布下一个 alpha 版本
qegg pre            # 查看当前渠道和下一个版本
qegg pre exit       # 退出渠道，下一次发布回到稳定版
```

- 锁文件里的 `channel` 是唯一权威；`version` 只是最近一次计划的快照，每次运行会按 tag 重算并回写。
- 版本 = 最后一个稳定版本按记录算出目标版本，再加 `-<channel>.<n>`；同一渠道连续发布时 n 递增，目标基线变大时从 1 重新开始。
- 预发布**不消费变更记录、也不写 `CHANGELOG.md`**，记录留到稳定版一起发布；只有新增记录才会产生下一个预发布号，避免空转发。
- 预发布的 Release notes 只列出本渠道上一次预发布之后新增的记录；稳定版才汇总自上一个稳定 tag 以来的全部记录，compare 链接也从该稳定 tag 起算。
- `qegg pre exit` 之后的下一次发布是同一基线的稳定版（`0.5.0-alpha.2` → `0.5.0`），会一次性写 CHANGELOG 并消费记录。
- GitHub Release 标成 Pre-release 且不会成为 Latest；npm 发布到同名的 dist-tag（`alpha` / `beta` / `rc`），不会覆盖 `latest`。
- Python 保留 `pyproject.toml` 里的写法（`2.0.2-a14`），tag 与 PyPI 用 `2.0.2a14`；每个单元按自己的 tag 序列计算 `n`。

## 多包仓库

- `version_scope = "package"`（默认）逐个包发布：每个包独立版本、独立 tag（默认 `{{slug}}-v{{version}}`，可用 `[git] tag_format` 覆盖）和独立 GitHub Release。
- `version_scope = "shared"` 让所有包共用一个版本与 tag，适合内部包必须同版本的仓库（例如 uv workspace 里 app 固定依赖兄弟包）；要求所有包当前版本相同、tag 拼写一致（有待发布计划时例外，见下）。Go 模块只能出现在 shared 里：它的版本来自 tag，而 shared 的 tag 正好是 `vX.Y.Z`。
- 只有命中本次发布包的记录才会被消费；`release: false` 记录留到对应包发布时再写入变更日志。
- 共享版本的 Python 工作区会自动把 `[project].dependencies` 里兄弟包的 `==` pin 改成新版本；仓库根有 `uv.lock` 时还会执行 `uv lock`。
- 共享版本的 Cargo workspace 写根 `[workspace.package].version`，同步兄弟 `path` 依赖的 `version`（缺的补上），并在有 `Cargo.lock` 时执行 `cargo update --workspace`。成员继承版本时只能用 shared：package scope 下多个继承成员会在发布时报错。
- 发布顺序按清单依赖与 `depends-on` 排序，被依赖的包先发布（`cargo publish` 要求依赖已经在 crates.io 上）。

`qegg init` 首次生成配置（默认登记扫描到的包）时，如果登记了两个及以上包、且各包的清单版本相同，会自动写 `version_scope = "shared"`；有包从根清单继承版本（Cargo workspace、Maven parent）时同样选 `shared`。其余情况保持 `package`，需要共用版本时手工写 `version_scope = "shared"`。多包仓库里有版本来自 tag 的包时，`qegg init` 不会拒绝配置，而是给出警告（`init.warn.tag_versioned_multi`）：这类包只能用 `shared`，并且所有包要从同一个版本开始。`qegg scan` 用同一套规则预测 `qegg init` 会写入的版本范围（输出里的 `version_scope`），两者不会给出不同的结果。

## 共享版本与 tag 版本包

`version_scope = "shared"` 可以让版本来自 tag 的包与清单里有版本的包共用一个版本、一个 tag（沿用 `tag_prefix`，Go 的 tag 因此以 `v` 开头）。两条约束：

- tag 版本包只能待在仓库根目录：Go 适配器只读根目录的 `go.mod`，一个仓库 tag 也只能承载一个版本。`qegg init` 不因此拒绝配置，只在多包仓库里提示这类包只能用 `shared`。
- `version_scope = "package"` 的多包仓库不接受 tag 版本包：每个包要独立 tag，而 Go 表达不了；`qegg plan` 与 `qegg check` 都会拒绝（check 的问题码是 `package-tag-versioned`）。

pr 模式下这类仓库分两次运行，两次运行之间各包的当前版本并不相等：

| 运行 | 时机 | 版本状态 | 产出 |
| --- | --- | --- | --- |
| version | 推送主分支 | 清单包写成新版本；tag 版本包仍是上一个 tag 的版本（没有 tag 时是 `0.0.0`） | release 分支上的版本提交（含 `.quailegg/release.json`）与 release PR |
| publish | 合并 release PR | 清单包已是新版本；tag 版本包仍报上一个 tag，直到新 tag 建好 | 构建、发布、打 tag、推 tag、建 GitHub Release |

`.quailegg/release.json` 是提交进仓库的发布意图（marker）：version 阶段写入待发布版本、各包版本、tag、本次消费的记录等；publish 阶段重新跑 `qegg plan` 时读它，按里面的版本校验各包，再据此构建、发布与打 tag。发布完成后 marker 保留在仓库里，不再约束后续版本，只作为预发布去重、转正式版和发布说明的基线。`commit` 模式在同一次运行里完成这两步，不存在这个版本差异。

版本一致性校验按“有没有待发布计划”分两种。待发布计划指 marker 里 tag 还不存在、且它覆盖的包都还登记着的发布，不要求 marker 版本与当前检出相符：

| 情况 | 清单包 | tag 版本包 |
| --- | --- | --- |
| 没有待发布计划：没有 marker、marker 里的发布都已打过 tag、它覆盖的包已不再全部登记，或同时有多个发布待发 | 必须与其他包严格相等 | 必须与其他包严格相等 |
| 有待发布计划 | 必须已经等于计划版本 | 不高于计划版本即可（它的 tag 在 publish 阶段才创建） |

- 待发布计划与检出不符时直接报错，而不是退化成严格相等：清单包的版本被改回去、tag 比计划新（已经发了 `v0.2.0`，却要发 `v0.1.0`）都属于这种情况。plan 报“包 <名称> 的版本是 <当前版本>，但待发布的版本是 <计划版本>”，check 报 `shared-version-pending`，并提示“重新发起发布，让计划与已检出的版本一致，或删除过期的 `.quailegg/release.json`”。
- 这条规则只作用于多包共享单元。单包仓库没有第二个包可比，待发布计划与检出不符时不会报错，而是直接重新规划：有记录时按当前检出算出新版本，marker 里的版本被忽略，版本阶段随后写入新的 marker；没有记录时计划为空，`qegg release` 打印“没有需要发布的变更记录”并以退出码 0 结束，发布阶段的预检不执行。过期的 marker 因此拦不住单包仓库的下一次发布。
- `qegg plan` 用本次要发布的版本校验共享单元，包括 tag 拼写；`qegg check` 不计算新版本，只能静态判断：有待发布计划时按计划版本、否则按当前版本比较版本对齐与 tag 拼写。release 分支上的 `qegg check --ci` 因此不会因为“清单包已是新版本、tag 版本包还是旧版本”而误报。
- `qegg check` 会读 marker（不限 shared 仓库）：marker 损坏时 check 直接失败（`marker-invalid`，提示修正或删除 `.quailegg/release.json` 后重试），读不到 tag 时报 `pending-unresolved`，提示“拉取发布标签（fetch-depth: 0）后重试”。它靠 `git tag` 判断 marker 里的发布是否已经打过 tag，浅克隆（`fetch-depth: 1`）里看不到历史 tag，已完成的 marker 会被当成待发布；生成的工作流用 `fetch-depth: 0`，手工运行 check 时要先取全 tag。
- 待发布计划没有覆盖新登记的包时报错：plan 报“待发布的版本 <版本> 没有覆盖 <包>”，check 报 `shared-version-uncovered`，都提示重新发起发布。旧格式 marker 只写 `version`、没有记录各包的发布范围时，待发布计划按当前登记的包展开，因此这条检查对它不生效。
- `[[version.file]]` 登记的额外版本文件按同一个预期版本校验，有待发布计划时用计划版本。读不到 marker 或无法解析待发布版本、因而无法确定预期版本时，check 报 `version-file-unverified` 并附上 marker 的问题，而不是把各包版本不一致误报成 `version-file-ambiguous`。
