# AGENTS.md

Instructions for agents working on this repository. QuailEgg is a Go release
automation tool, and this repository releases itself with it: a change becomes a
record in `.changes/`, the records become a version bump, a `CHANGELOG.md`
section, a git tag and a GitHub Release.

Precedence: `.quailegg.toml` and the code beat this file for mechanics (commands,
paths, allowed values). This file wins for editorial rules (commit subjects,
record prose). When the two disagree, fix this file.

## Commands

| Command | Purpose |
| --- | --- |
| `go build ./...` | build |
| `go vet ./...` | static check |
| `gofmt -l .` | formatting: must print nothing |
| `go test ./...` | unit and e2e tests |
| `qegg scan` | packages `qegg init` can register |
| `qegg check` | config, ecosystem, tools and records |
| `qegg plan` | next version and grouped changes |
| `qegg plan --markdown` | the same, ready for a PR comment |
| `qegg verify` | run `[verify].commands` by hand |
| `qegg hook pre-commit` | run exactly what the git hook runs |
| `qegg status` | publish state, `--offline` to skip registry calls |

Go 1.26 (see `go.mod`); CI uses the same version. Tests need no push or registry
credentials: they copy fixtures from `internal/ecosystem/testdata` and use a bare
local repository as the origin.

The docs site is a separate pnpm project: `cd site && pnpm install`, then
`pnpm dev` or `pnpm build`.

## Repository map

- `cmd/qegg/`: process entry, calls `cli.Execute` and nothing else.
- `internal/cli/`: cobra commands, one file per command, `Runtime` and `ExitError`.
- `internal/app/`: use cases (init, scan, check, commit, plan, release, resume, status).
- `internal/release/`: plan, executor, version and publish phases, marker, changelog section.
- `internal/ecosystem/{npm,python,rust,golang}/`: adapters; `ecosystem/registry` is the only entry point.
- `internal/changes/`: record parsing, writing, and the pre-commit rules.
- `internal/scaffold/`: `qegg init` plan and generated assets.
- `internal/i18n`, `config`, `semver`, `git`, `github`, `versionfile`, `platform`: shared leaves.
- `test/e2e/`: end-to-end tests; `site/`: rspress documentation site.

Dependency direction is `cmd -> cli -> app -> release -> {state, verify, registry,
github, changes, changelog, config, semver, platform}`. Ecosystem sub-packages may
depend on `ecosystem` only; `platform/*`, `i18n` and `versionfile` are leaves and
must not depend upwards. Core packages (`app`, `release`, `scaffold`) must not name an ecosystem:
ask `ecosystem/registry` for the capability. `internal/ecosystem/registry/policy_test.go`
guards this. Details in `docs/architecture.md`.

A Go file stays under 1000 lines. User-visible strings live only in
`internal/i18n/locales/{en,zh}.toml`; a test guards that.

## Change workflow

1. Make the code change.
2. Stage everything first. `qegg commit` commits the index, so unstaged code
   would be left out of the commit.
3. Write one record per logical change and commit it with the code:

   ```bash
   git add -A
   qegg commit \
     -m "feat(changes): reject duplicate record slugs" \
     --body "Reject a record whose slug is already used, so two records never fight over one file name." \
     --packages github.com/SuperQuail/QuailEgg:minor
   ```

   `--no-commit` writes the record only; commit it yourself if you need a
   different message. Never commit a record without the code it describes.

   One commit adds at most one record. Generate records with `qegg commit`; if
   you must hand-write one, commit it alone and say in the commit body why
   `qegg commit` was not used. Release notes link each record to the commit
   that introduced it, so batching several records into one commit collapses
   their links onto one hash.
4. The pre-commit hook does two things: it rejects a commit that touches
   `[changes].scope` without a valid record, then runs `[verify].commands`
   (`go test ./...`, 15 minute timeout) only when the commit touches a file
   inside `[changes].scope` (an empty scope means the whole repository). Let it
   run; do not bypass it.
   The record rule only applies on the project clone's default branch: the
   hook compares the `origin` remote with `[project] repo` from the config, so
   feature branches and fork clones are left to the pull request check.
5. Open a PR. CI runs `qegg check --ci` on it and posts the plan as a comment.

A record is required for changes under the directories in `[changes].scope` of
`.quailegg.toml`. Docs, site and CI-only changes need no record, but they still
follow the commit rules below.

## Change records

- Location: `.changes/<slug>.md`. Let `qegg commit` name the file. A hand-written
  name uses lowercase letters, digits and dashes, kept short: the current
  validator rejects more than three dash-separated words.
- Front matter: `type` is required and is one of feat, fix, perf, refactor, docs,
  test, build, ci, chore, style, revert. `scope` is optional. `packages` is
  required and maps a registered package name from `.quailegg.toml` to
  `patch`, `minor` or `major`. `release` is optional and defaults to true.
- `release: false` means the entry appears in the changelog without bumping a
  version. Use it for changes worth announcing that do not change behaviour.
- Body: one sentence, English, imperative, ending with a period, at most 120
  characters. Describe what a user can now do, not how it was implemented.
- No lists, code blocks, headings or multiple paragraphs. No file names, package
  names, tool names or internal jargon. One main action per sentence; do not join
  two independent statements with a comma.
- The body is copied verbatim into `CHANGELOG.md`. It is release-note prose, not
  a commit subject.
- `.changes/README.md` and `.quailegg.toml` are generated: `qegg init --force`
  owns them, do not edit by hand. The config records the project repository as
  `[project] repo`, which is how the hook tells a fork clone apart. The same command appends a managed block to
  `.gitignore` that keeps `.quailegg/state/`, `.quailegg/lock` and
  `.quailegg/assets/` out of git; leave that block in place.
- The release commit deletes consumed records. Never delete a record yourself.

Example:

```markdown
---
type: feat
scope: changes
packages:
  github.com/SuperQuail/QuailEgg: minor
---

Reject a record whose slug is already used, so two records never fight over one file name.
```

## Commit messages

Format: `type(scope): imperative summary`, for example
`fix(release): keep the marker when publishing resumes`.

- The type reflects the intent, not the file touched: test-only changes are
  `test`, workflow changes are `ci`, documentation-only changes are `docs`.
- Scope is optional and comes from the list below. Omit it when a change spans
  several areas.
- The summary is lowercase, imperative, has no trailing period, targets 50
  characters and never exceeds 72. No emoji, no file names, no tool names, no
  module paths.
- One commit is one logical change. If the subject needs "and", split the commit.
- The body explains why the change exists and what it affects. Wrap at 72
  columns; bullets are fine here.
- The changelog sentence goes into `--body`. Without `--body` the subject is
  copied verbatim into the changelog, which is almost never what you want.
- Subject and changelog sentence must agree: the subject is the short action, the
  `--body` text is the user-facing sentence.

| Scope | Area |
| --- | --- |
| `cli` | `cmd/qegg`, `internal/cli`, `internal/tui` |
| `app` | `internal/app` |
| `release` | `internal/release`, `internal/release/state`, `internal/verify` |
| `assets` | `internal/release/assets` |
| `changes` | `internal/changes` |
| `changelog` | `internal/changelog` |
| `config` | `internal/config`, `.quailegg.toml` |
| `ecosystem` | `internal/ecosystem/**` |
| `scaffold` | `internal/scaffold/**` |
| `docs` | `README.md`, `README.CN.md`, `docs/`, `AGENTS.md`, `CONTRIBUTING.md` |
| `site` | `site/` |
| `ci` | `.github/workflows/`, workflow pins |
| `deps` | `go.mod`, `go.sum`, `site/package.json` |

Good and bad examples:

| Bad | Why | Good |
| --- | --- | --- |
| `fix: isolate tests` | a test change, invisible to users | `test(release): isolate tests from the CI environment` |
| `chore(release): pin qegg v0.2.0 and translate changelog` | two unrelated changes | `ci: pin qegg to v0.2.0` plus `docs(changelog): translate headings to English` |
| `docs(readme): add english version and README.CN.md` | file name in the subject, "and" | `docs: add English README` plus `docs: rename README.CN.md to README.zh-CN.md` |
| `feat(assets): build release assets` | implementation verb, tool name | `feat(assets): publish cross-compiled binaries with checksums` |
| `feat: implement quailegg release tool` | says "implement", nothing a user can do | `feat: add tag-based release flow` |

## Generated commits and files

- `chore(release): vX.Y.Z` is produced by `qegg release` from the
  `commit_message` template in `.quailegg.toml`. Never hand-write, amend or
  reorder it: it deletes records, rewrites `CHANGELOG.md` and updates
  `.quailegg/release.json`.
- Upgrading the pinned toolchain is `ci: pin qegg to vX.Y.Z` and touches the
  workflow pins only. Fixing changelog prose afterwards is a separate
  `docs(changelog): ...` commit.
- `develop = true` in `.quailegg.toml` makes the generated workflows build qegg
  from the checked-out QuailEgg source (other repositories install `@main`). It
  is a debugging mode, not a reproducible pin: turn it off with
  `qegg init --force --develop=false --qegg-version vX.Y.Z` and commit the
  generated files as `ci: pin qegg to vX.Y.Z` before a stable release.
- `CHANGELOG.md` between `<!-- qegg:release X -->` and `<!-- qegg:release:end -->`
  is generated. Never edit inside those markers; prose that must change now is
  edited outside them, everything else waits for the next release.
- The hook and the three workflows are generated from `internal/scaffold/assets/`.
  Change the template, not the generated file. Templates use `[[ ]]` actions and
  must stay LF.
- The generated release workflow runs only on release paths; the generated pull
  request workflow skips the release branch, whose verification is the publish
  run. With `[[release.build]]`, a leading plan job gates the platform builds on
  the publish phase, so preparing a version never builds.
- `CHANGELOG.md` headings follow the user's language setting, not the repository:
  run `qegg --lang en release` or set `QEGG_LANG=en` so released headings stay
  English.

## Release

- Two modes: `commit` (the release workflow commits on main) and `pr` (the
  workflow opens a release PR). This repository uses `mode = "pr"`: a push to
  main carrying a record opens a release PR, and merging that PR publishes.
- Prereleases are switched with `qegg pre alpha|beta|rc|exit`; the channel lives
  in the committed `.quailegg/pre-release.lock`. A prerelease never writes
  `CHANGELOG.md` and never consumes change records; the stable release that
  follows `qegg pre exit` does both.
- Prerelease notes list only the entries added since the previous prerelease.
  The stable release aggregates every record since the last stable tag and
  links its compare range to that tag.
- Let CI release. In an emergency `qegg release --yes` works locally, but never
  while the release workflow is running.
- If a release is interrupted, run `qegg resume`; do not re-tag or re-push by
  hand.
- Never create tags, GitHub Releases or registry publishes manually.
- The release commit is authored by `github-actions[bot]`; the workflow skips
  itself for that author. Never include a release commit in a pull request.

## Documentation

- `README.md` is English, `README.CN.md` is the Chinese mirror; update both in
  one commit.
- `site/docs/` is Chinese. Run `cd site && pnpm build` after editing it; the site
  is not needed when changing Go code.
- Behaviour documents: `docs/architecture.md` for layout and dependency
  direction, `docs/ecosystems.md` for adapters, `docs/releasing.md` for the
  bootstrap and release flow.

## Before you commit

- [ ] `gofmt -l .` prints nothing, `go vet ./...` is clean, `go test ./...` passes.
- [ ] One record per logical change, body is one user-facing sentence under 120
      characters.
- [ ] Subject is imperative, lowercase, under 72 characters, contains no "and"
      joining two changes and no file names.
- [ ] `qegg plan` shows the expected bump and the changelog line you intended.
- [ ] `git status` lists only intended files, and generated files came from qegg
      rather than from an editor.

## Never

- Force-push `main` or rewrite published history; tags and release commits are
  immutable.
- Use `git commit --no-verify` unless genuinely blocked, and say why in the
  commit body.
- Edit `CHANGELOG.md` inside the generated markers, `.changes/README.md` or
  `.quailegg.toml` by hand.
- Hand-write `chore(release):` commits, tags or GitHub Releases.
