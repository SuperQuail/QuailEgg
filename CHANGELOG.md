# Changelog

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
