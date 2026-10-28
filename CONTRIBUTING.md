# Contributing

## 开发

```bash
go build ./...      # 构建
go vet ./...        # 静态检查
go test ./...       # 测试
```

`qegg init` 会安装 `.git/hooks/pre-commit`：先要求有变更记录，再执行 `[verify].commands`（本仓库是 `go test ./...`）。校验失败会拒绝提交；只想跳过 hook 时用 `git commit --no-verify`。

本地联调可以在临时仓库里跑完整流程：

```bash
go build -o qegg ./cmd/qegg
mkdir /tmp/demo && cd /tmp/demo && git init
# 创建 package.json 或 pyproject.toml 后：
/path/to/qegg init --no-input --mode commit --registries npm --no-hook
/path/to/qegg commit --no-input -m "feat: first change" --packages <包名>:minor
/path/to/qegg plan
/path/to/qegg release --no-publish --no-push --no-github-release
```

需要覆盖 tag 与推送时，用本地 bare 仓库当 origin，不需要任何远端凭据：

```bash
git init --bare /tmp/demo-origin.git
git remote add origin /tmp/demo-origin.git
git push origin HEAD
/path/to/qegg release --yes --no-publish --no-github-release
git --git-dir=/tmp/demo-origin.git tag
```

## 约定

- 每个用户可见改动都在 `.changes/` 新增一条记录。
- 提交信息使用约定式提交。
- 生成的 git hook 与 Actions 工作流以文件形式放在 `internal/scaffold/assets/`：需要变量替换时用 `.tmpl` 与 `[[ ]]` 动作，不在 Go 里拼接正文；生态的 setup 步骤放在各生态包的 `workflow-setup.yml`。
- 代码遵循 `gofmt`；包依赖方向见 `docs/architecture.md`，生态扩展见 `docs/ecosystems.md`。
