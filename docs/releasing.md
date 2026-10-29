# 自举与首发

qegg 自己用 qegg 发布。这个仓库的版本号来自 git tag（Go 生态），所以"第一个版本怎么来"和"发布时用哪个 qegg"需要按下面的顺序走。

## 版本号规则

- Go module 没有版本字段，版本就是 **可达 HEAD 的最高 `v*` tag**；没有 tag 时视为 `0.0.0`，因此首个 `feat` 记录得到 `v0.1.0`，首个 `patch` 记录得到 `v0.0.1`。
- 生成的 workflow 安装 `@latest`（`go install ...@latest`），每次 CI 都用最新的已发布版本。发布流程只依赖已发布版本，**不会、也不能引用正在发布的版本**，所以不存在版本号自指或循环。
- `@latest` 表示不可复现：新版本发布后 CI 自动用上它，新版本有缺陷时 CI 也会跟着坏；需要固定时执行 `qegg init --force --qegg-version vX.Y.Z`，把三个 workflow 的 pin 写死。

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

## 常见失败

| 现象 | 处理 |
| --- | --- |
| workflow 里 `go install ...@vX` 401/404 | 仓库需公开；私有模块要配置 `GOPRIVATE` 与凭据（当前不支持） |
| `qegg status` 显示 published: no | 预检请求本身会触发 proxy 抓取；刚 push 的 tag 第一次可能仍是 no，重试一次即可。持续为 no 时检查模块路径大小写与 proxy 可达性 |
| 发布卡在 push | 分支保护不允许 GITHUB_TOKEN 推送；改用本地 `qegg release --yes` 或调整保护规则 |
| CI 无法创建发布 PR | 仓库设置里允许 GitHub Actions 创建 PR，或本地以 pr 模式发版 |
| 大版本升级报错 | v2+ 需要 `/vN` module 路径，先改 `go.mod` 与 import 再发版 |
| 无 tag 时版本是 0.0.0 | 正常；首个版本用 `feat` 记录即可得到 0.1.0 |

## PR 模式

PR 模式下 release 分支是生成物：每次 push 到主分支，CI 都从主分支最新提交重建它，
提交版本号和 CHANGELOG 后 force push，并刷新同一个 release PR。功能 PR 合并只会让它变长，
主分支上的版本要等你合并 release PR 才更新；合并后新一轮 CI 打 tag、发布，
并把 release 分支拉平到主分支。

- release PR 用 squash 合并，标题固定为 `chore(release): bump versions`。
- 建议主分支开启 Require branches to be up to date before merging，避免过期的 release PR 被合入。
- release 工作流只在发布相关路径变化时运行（`.changes/**`、`.quailegg.toml`、`.quailegg/pre-release.lock`、`.quailegg/release.json`、`CHANGELOG.md` 与各生态清单），只改文档的推送不会重建 release 分支。
- release PR 由 `github-actions[bot]` 用 GITHUB_TOKEN 创建，GitHub 不会为它正常启动检查：PR 上可能出现 `action_required`，或一个 0 job 的启动失败 run（合并/强推后尤其明显）。`qegg-pr` 对 release 分支加了 `if: github.head_ref != 'release'`，run 一旦真正启动就跳过；不要把这条检查设为必需，验证由 publish 那次 run 负责。需要真实检查只能换成 PAT/GitHub App 身份。
- workflow 安装 `@latest`，新版本发布后 CI 自动升级；要固定版本时用 `qegg init --force --qegg-version vX.Y.Z`。
- CHANGELOG 的贡献者句柄取自 PR（没有 PR 时取 commit）作者的 GitHub login；私有仓库本地需要 `GITHUB_TOKEN`/`GH_TOKEN` 或登录过的 `gh`，都不可用时省略作者，不写显示名，也不影响发版。

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
- `qegg pre exit` 之后的下一次发布是同一基线的稳定版（`0.5.0-alpha.2` → `0.5.0`），会一次性写 CHANGELOG 并消费记录。
- GitHub Release 标成 Pre-release 且不会成为 Latest；npm 发布到同名的 dist-tag（`alpha` / `beta` / `rc`），不会覆盖 `latest`。
- Python 包暂不支持预发布，计划阶段会直接报错；Go 与 crates 不需要额外处理。
