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
| `qegg status` 显示 published: no | proxy 尚未缓存该版本，push tag 后首次 `go get` 才会填充，与 tag 是否存在无关 |
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
- release 工作流只在发布相关路径变化时运行（`.changes/**`、`.quailegg.toml`、`.quailegg/release.json`、`CHANGELOG.md` 与各生态清单），只改文档的推送不会重建 release 分支。
- release PR 由 `github-actions[bot]` 用 GITHUB_TOKEN 创建，`qegg-pr` 工作流会跳过 release 分支（`if: github.head_ref != 'release'`）；不要给它配置必需检查，验证由 publish 那次 run 负责。
- workflow 安装 `@latest`，新版本发布后 CI 自动升级；要固定版本时用 `qegg init --force --qegg-version vX.Y.Z`。
