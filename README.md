# QuailEgg

English | [中文](README.CN.md)

QuailEgg is an automated release tool. Once changes land on the main branch it collects the records in `.changes/`, computes the next version, updates `CHANGELOG.md`, publishes the packages and creates the Git tag and GitHub Release.

## Features

- Release records drive the flow, a pre-commit hook checks the record and runs `[verify].commands`
- Detects npm, PyPI, crates.io and Go modules automatically
- Release as a pull request or a direct commit, interrupted runs resume safely
- Prerelease channels (`alpha`, `beta`, `rc`) that keep the records for the stable release
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

`qegg init` needs a configured `origin` remote: the repository identity is written to `[project] repo`, which lets the pre-commit hook tell a fork clone apart.

Releases open a pull request by default; set `[release].mode = "commit"` in `.quailegg.toml` to commit directly.

## Prereleases

```bash
qegg pre alpha      # start an alpha line
qegg pre            # show the channel and the next version
qegg pre exit       # graduate to the stable release
```

The channel lives in the committed `.quailegg/pre-release.lock`. A prerelease never writes `CHANGELOG.md` and never consumes change records; the stable release after `qegg pre exit` does both. GitHub Releases are marked pre-release, and npm publishes under the channel dist-tag.

## Commands

| Command | Description |
| --- | --- |
| `qegg init` | Generate config, hook and workflows |
| `qegg commit` | Write and commit a change record |
| `qegg check` | Validate environment and config |
| `qegg plan` | Preview the release plan |
| `qegg verify` | Run the pre-release verification |
| `qegg changelog` | Generate CHANGELOG.md from `.changes/` |
| `qegg pre` | Start, stop or show a prerelease channel |
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
repo = "github.com/you/project"   # written by qegg init

[publish]
registries = ["npm"]

[packages.my-package]
path = "."
resolver = "npm"    # npm | python | rust | go
```

Missing values come from detection or defaults; release credentials go through environment variables or CI secrets.

## License

[MIT](LICENSE)
