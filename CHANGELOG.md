# Changelog

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
