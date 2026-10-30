# Changelog

<!-- qegg:release 0.9.0 -->
## v0.9.0 - 2026-10-30

### Features

- [`ddb7e78`](https://github.com/SuperQuail/QuailEgg/commit/ddb7e78b59c2636c91af294cb5cd188aa88a3027): Run custom GitHub Actions steps in the release workflow.

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.8.0...v0.9.0
<!-- qegg:release:end -->

<!-- qegg:release 0.8.0 -->
## v0.8.0 - 2026-10-30

### Features

- [`a5c7efa`](https://github.com/SuperQuail/QuailEgg/commit/a5c7efa9318e57d01a3c2b0cfa12063ee8d70ed9): Build release assets from configured commands and upload the collected files. ([#14](https://github.com/SuperQuail/QuailEgg/pull/14) by @Wsrsq)
- [`a5c7efa`](https://github.com/SuperQuail/QuailEgg/commit/a5c7efa9318e57d01a3c2b0cfa12063ee8d70ed9): Run platform build jobs before the release and attach their output. ([#14](https://github.com/SuperQuail/QuailEgg/pull/14) by @Wsrsq)

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.7.0...v0.8.0
<!-- qegg:release:end -->

<!-- qegg:release 0.7.0 -->
## v0.7.0 - 2026-10-30

### Features

- [`5075f9c`](https://github.com/SuperQuail/QuailEgg/commit/5075f9cade0368621185bd92c1765338557f967d): Read release packages from their configured paths, so standalone nested projects can join the release. ([#12](https://github.com/SuperQuail/QuailEgg/pull/12) by @Wsrsq)
- [`1a1110f`](https://github.com/SuperQuail/QuailEgg/commit/1a1110f90e63ea02a5de6a172dfdb79e91f9d96d): Release a multi-language workspace from one version, keeping every file that carries it in sync. ([#13](https://github.com/SuperQuail/QuailEgg/pull/13) by @Wsrsq)

### Bug Fixes

- [`f20ff31`](https://github.com/SuperQuail/QuailEgg/commit/f20ff316ad79022556d7a75f001a5d340d2c3831): Release Cargo workspaces that inherit their version from the workspace root. ([#9](https://github.com/SuperQuail/QuailEgg/pull/9) by @Wsrsq)

### Refactors

- [`3c7fab5`](https://github.com/SuperQuail/QuailEgg/commit/3c7fab54726b27a65353e8fa016a1f61152f966b): Drive init and release planning from declared ecosystem capabilities. ([#11](https://github.com/SuperQuail/QuailEgg/pull/11) by @Wsrsq)

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.6.1...v0.7.0
<!-- qegg:release:end -->

<!-- qegg:release 0.6.1 -->
## v0.6.1 - 2026-10-30

### Bug Fixes

- [`26a7b97`](https://github.com/SuperQuail/QuailEgg/commit/26a7b976d1e5905561484884c017fde9a0954c0a): Skip the plan comment when there is nothing to release, so documentation-only pull requests pass. ([#7](https://github.com/SuperQuail/QuailEgg/pull/7) by @Wsrsq)

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.6.0...v0.6.1
<!-- qegg:release:end -->

<!-- qegg:release 0.6.0 -->
## v0.6.0 - 2026-10-30

### Features

- [`07aa477`](https://github.com/SuperQuail/QuailEgg/commit/07aa477dd338fb23097b70bac968fe6e58bf71ac): Run the pre-commit verification commands only when a commit touches release-scoped files. ([#4](https://github.com/SuperQuail/QuailEgg/pull/4) by @Wsrsq)
- [`285b5b0`](https://github.com/SuperQuail/QuailEgg/commit/285b5b04ec401a234e8727dabb6dfdb44c9d33d8): Release multi-package workspaces per package or with one shared version, including Python prereleases. ([#3](https://github.com/SuperQuail/QuailEgg/pull/3) by @Wsrsq)

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.5.2...v0.6.0
<!-- qegg:release:end -->

<!-- qegg:release 0.5.2 -->
## v0.5.2 - 2026-10-29

### Bug Fixes

- [`6f9bcf6`](https://github.com/SuperQuail/QuailEgg/commit/6f9bcf63c3e86b596b05e3a5a66bb1a06dc4e6a8): Show contributor logins in the changelog instead of profile display names. ([#2](https://github.com/SuperQuail/QuailEgg/pull/2) by @Wsrsq)

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.5.1...v0.5.2
<!-- qegg:release:end -->

<!-- qegg:release 0.5.1 -->
## v0.5.1 - 2026-10-29

### Bug Fixes

- [`a15194a`](https://github.com/SuperQuail/QuailEgg/commit/a15194aeb514d6bfc3f333448c2cf9db93357056): Show the real published state of a Go module in the status report. ([#1](https://github.com/SuperQuail/QuailEgg/pull/1) by @Wsrsq)

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.5.0...v0.5.1
<!-- qegg:release:end -->

<!-- qegg:release 0.5.0 -->
## v0.5.0 - 2026-10-29

### Features

- [`5dde5c8`](https://github.com/SuperQuail/QuailEgg/commit/5dde5c868bc2074e777a738dafc9fc327c1f0eb9): Require a configured remote before scaffolding a repository.
- [`67a7582`](https://github.com/SuperQuail/QuailEgg/commit/67a7582ddaf06c1111d25bea13ec4e977799dc04): Switch prerelease channels with qegg pre alpha, beta, rc or exit.
- [`2e153c7`](https://github.com/SuperQuail/QuailEgg/commit/2e153c7f208d3f9d0bd95e7e4d4f51ea87fe53df): Record the prerelease channel in a committed lock file.
- [`0ede957`](https://github.com/SuperQuail/QuailEgg/commit/0ede957ce7a1d9bd007ce23d721a68db27a83346): Trigger releases when the prerelease lock changes.
- [`6d8ffd5`](https://github.com/SuperQuail/QuailEgg/commit/6d8ffd5ddbd6c20a4c295d63f979ed44c12e0dc1): Plan prerelease versions from the committed channel lock.
- [`9bc39c5`](https://github.com/SuperQuail/QuailEgg/commit/9bc39c58ded2c9daef56464ad86715a7c25f210f): Publish prerelease versions without replacing the latest release.
- [`f94a4da`](https://github.com/SuperQuail/QuailEgg/commit/f94a4da82cbf8fcb58458ab73d84b898537a0d01): Number prerelease versions in the alpha, beta and rc channels.
- [`14fdac5`](https://github.com/SuperQuail/QuailEgg/commit/14fdac50591802ae5576826458b7ef07d348374b): Record the project repository in the configuration.

### Bug Fixes

- [`6ba4d31`](https://github.com/SuperQuail/QuailEgg/commit/6ba4d31848a896a5dd65bb658bc0a42c9ce1b4a6): Skip the change record rule outside the project repository.
- [`184817b`](https://github.com/SuperQuail/QuailEgg/commit/184817bdb91619ef83c518962b22178c5be4048d): Ignore unknown fields so older binaries keep reading newer qegg files.

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.4.0...v0.5.0
<!-- qegg:release:end -->

<!-- qegg:release 0.4.0 -->
## v0.4.0 - 2026-10-29

### Features

- [`b2176b9`](https://github.com/SuperQuail/QuailEgg/commit/b2176b9a12879e18c2897976fe312b579e3ed293): Keep qegg runtime state out of version control when scaffolding a repository.
- [`ed5b4a1`](https://github.com/SuperQuail/QuailEgg/commit/ed5b4a1b81d7c478898afa92374ce53bef93ed48): Run the release workflow only when release paths change.

### Bug Fixes

- [`7625841`](https://github.com/SuperQuail/QuailEgg/commit/7625841f31cf1312e23bfdac36f1d0f3a347f30d): Skip the pull request check on the generated release branch.
- [`82f5858`](https://github.com/SuperQuail/QuailEgg/commit/82f58586980f4247dcce4832734d1fe33ed0a667): Link each changelog entry to the pull request that introduced it.
- [`b1959cd`](https://github.com/SuperQuail/QuailEgg/commit/b1959cd8d71121320d6104b58e89ec9fd8526a56): Report the real slug word limit when a change record name is rejected.

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.3.0...v0.4.0
<!-- qegg:release:end -->

<!-- qegg:release 0.3.0 -->
## v0.3.0 - 2026-10-29

### Features

- [`cd080d5`](https://github.com/SuperQuail/QuailEgg/commit/cd080d583a8139210e91c0a7465732c34175eb2f): Link each changelog entry to the pull request that introduced it.
- [`c951103`](https://github.com/SuperQuail/QuailEgg/commit/c9511034d816fbee738ddbabc905e4bb59eb8b0c): Rebuild the release pull request on every push so merged changes share one version.

### Bug Fixes

- [`7ddf284`](https://github.com/SuperQuail/QuailEgg/commit/7ddf284c5a6b7b0e024e296057648f326e450721): Clear inherited git hook variables before verification and in git tests, so nested commands cannot touch the repository running the hook.

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.2.0...v0.3.0
<!-- qegg:release:end -->

<!-- qegg:release 0.2.0 -->
## v0.2.0 - 2026-10-28

### Features

- [`771a4ea`](https://github.com/SuperQuail/QuailEgg/commit/771a4ea1d651082fd3573ef3300631fd04679b33): Cross compile Go binaries for five platforms, package them with checksums and upload them to the GitHub Release.

**Full Changelog**: https://github.com/SuperQuail/QuailEgg/compare/v0.1.0...v0.2.0
<!-- qegg:release:end -->

<!-- qegg:release 0.1.0 -->
## v0.1.0 - 2026-10-28

### Features

- [`8767ef2`](https://github.com/SuperQuail/QuailEgg/commit/8767ef2bf6307e038df9fc0209d3b7727731c90e): Implement quailegg with tag based Go releases and a pre-commit test gate.

### Bug Fixes

- [`8785fc6`](https://github.com/SuperQuail/QuailEgg/commit/8785fc6f9268727d5e099f3263ca07fa921c1e34): Isolate release tests from the CI environment, so they pass on GitHub Actions.
<!-- qegg:release:end -->
