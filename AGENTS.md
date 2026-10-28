# AGENTS.md

Instructions for agents working on this repository.

## Change records

Every change under `cmd/`, `internal/` or `test/` needs one file in `.changes/`. Create it with `qegg commit` or by hand. The pre-commit hook rejects a commit without a staged record, then runs `go test ./...`.

### File name

`slug.md`, lower case letters, digits and dashes, at most two words. For example `cache-expiry.md`.

### Front matter

```markdown
---
type: feat
scope: cache
packages:
  github.com/SuperQuail/QuailEgg: minor
---
```

- `type` is required, one of feat, fix, perf, docs, refactor, test, build, ci, chore, style or revert.
- `scope` is optional.
- `packages` is required, maps a registered package to patch, minor or major.
- `release` is optional, defaults to true, a false record only appears in the changelog.

### Body

- If one sentence describes the change, do not write two. One to four sentences is the limit, fewer is better.
- Write English prose without lists, using only commas and periods, never colons or semicolons.
- Describe what changed for a user, not how it was implemented.

Example:

```text
Add Go module support, the release version now comes from git tags.
```
