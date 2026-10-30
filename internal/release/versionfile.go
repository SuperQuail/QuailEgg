package release

import (
	"errors"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// singleReleaseVersion returns the one version of a plan. A plan that bumps
// several versions cannot say which of them a version file should carry, so the
// configuration only allows the files when a single version is released.
func singleReleaseVersion(plan *Plan) (string, bool) {
	versions := make(map[string]bool, len(plan.Releases))
	for _, release := range plan.Releases {
		versions[release.Version] = true
	}
	if len(versions) != 1 {
		return "", false
	}
	for version := range versions {
		return version, true
	}
	return "", false
}

// versionFileWarnings reports configured version files that do not hold the
// version the release starts from. qegg check turns the same drift into a
// failure; a plan only warns, because the version phase rewrites the file.
func versionFileWarnings(root string, files []config.VersionFile, expected string) []string {
	if len(files) == 0 || expected == "" {
		return nil
	}
	warnings := make([]string, 0, len(files))
	for _, file := range files {
		current, err := file.Read(root)
		if err != nil {
			warnings = append(warnings, i18n.Tf("release.warn.version_file_unreadable", file.Path, err))
			continue
		}
		if current != expected {
			warnings = append(warnings, i18n.Tf("release.warn.version_file_drift", file.Path, current, expected))
		}
	}
	return warnings
}

// writeVersionFiles keeps the configured files that carry the release version
// in step with the manifests. The written paths join the release commit.
func (e *executor) writeVersionFiles() error {
	files := e.d.Cfg.Version.File
	if len(files) == 0 {
		return nil
	}
	version, ok := singleReleaseVersion(e.plan)
	if !ok {
		return errors.New(i18n.T("release.err.version_file_ambiguous"))
	}
	for _, file := range files {
		current, err := file.Read(e.d.Root)
		if err != nil {
			return err
		}
		if current == version {
			continue
		}
		if err := file.SetVersion(e.d.Root, version); err != nil {
			return err
		}
		rel, err := file.RepoPath()
		if err != nil {
			return err
		}
		e.syncPaths = append(e.syncPaths, rel)
		e.d.UI.Info("%s: %s -> %s", rel, current, version)
	}
	return nil
}
