# QuailEgg

English | [中文](README.CN.md)

QuailEgg is an automated release tool. Once changes land on the main branch it collects the records in `.changes/`, computes the next version, updates `CHANGELOG.md`, publishes the packages and creates the Git tag and GitHub Release.

## Features

- Release records drive the flow, a pre-commit hook checks the record and runs `[verify].commands`
- Detects npm, PyPI, crates.io and Go modules automatically
- Release as a pull request or a direct commit, interrupted runs resume safely
- `qegg init` generates GitHub Actions workflows, merging is enough to release

## Install

```bash
go install github.com/SuperQuail/QuailEgg/cmd/qegg@latest
```

## Quick start

```bash
qegg init       # config, git hook and GitHub Actions workflows
qegg commit     # write one change record and commit it
qegg release    # publish
```

Releases open a pull request by default; set `[release].mode = "commit"` in `.quailegg.toml` to commit directly.

## Commands

| Command | Description |
| --- | --- |
| `qegg init` | Generate config, hook and workflows |
| `qegg commit` | Write and commit a change record |
| `qegg check` | Validate environment and config |
| `qegg plan` | Preview the release plan |
| `qegg verify` | Run the pre-release verification |
| `qegg changelog` | Generate CHANGELOG.md from `.changes/` |
| `qegg release` | Run the release |
| `qegg resume` | Resume an interrupted release |
| `qegg status` | Show release status |
| `qegg lang` | Set the interface language (English by default) |

## Change records

Add one Markdown file under `.changes/` for every change:

```markdown
---
type: feat
packages:
  my-package: minor
---

One or two sentences about the change.
```

`qegg commit` writes it for you. See [AGENTS.md](AGENTS.md) for all fields and writing rules.

## Configuration

Create `.quailegg.toml` in the repository root:

```toml
schema_version = 1

[project]
default_branch = "main"

[publish]
registries = ["npm"]

[packages.my-package]
path = "."
resolver = "npm"    # npm | python | rust | go
```

Missing values come from detection or defaults; release credentials go through environment variables or CI secrets.

## License

[MIT](LICENSE)
