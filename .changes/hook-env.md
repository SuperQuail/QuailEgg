---
type: fix
scope: app
packages:
  github.com/SuperQuail/QuailEgg: patch
---

Clear inherited git hook variables before verification and in git tests, so nested commands cannot touch the repository running the hook.
