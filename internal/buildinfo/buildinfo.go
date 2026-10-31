// Package buildinfo reports the qegg build that produced a release state.
package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// releaseVersionPattern matches a stable semver triple.
var releaseVersionPattern = regexp.MustCompile(`^[0-9]+.[0-9]+.[0-9]+$`)

// versionOverride is injected through -ldflags -X main.version at build time.
var versionOverride string

// SetVersion records the version the process entry point received.
func SetVersion(version string) {
	versionOverride = version
}

// Version returns the module version of the running binary. A source build
// reports "devel"; a module-installed build reports its module version.
func Version() string {
	if version := strings.TrimSpace(versionOverride); version != "" {
		return strings.TrimPrefix(version, "v")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "devel"
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}

// Commit returns the short VCS revision embedded by go build, or "" when the
// binary was installed from a module version and carries no VCS stamp.
func Commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key != "vcs.revision" {
			continue
		}
		revision := strings.TrimSpace(setting.Value)
		if len(revision) > 7 {
			return revision[:7]
		}
		return revision
	}
	return ""
}

// InstallVersion is the qegg version pinned in generated workflows. A stable
// release pins its tag; every other build keeps the previous "latest" default.
func InstallVersion() string {
	version := strings.TrimPrefix(Version(), "v")
	if !releaseVersionPattern.MatchString(version) {
		return "latest"
	}
	return "v" + version
}
