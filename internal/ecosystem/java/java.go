package java

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// buildKind names the build tool of a package.
type buildKind int

const (
	buildMaven buildKind = iota
	buildGradle
)

// gradleManifests lists the Gradle build files in preference order.
var gradleManifests = []string{"build.gradle.kts", "build.gradle"}

// manifestFor returns the build file of dir and its kind. Maven wins when a
// directory carries both build systems.
func manifestFor(dir string) (string, buildKind, bool) {
	if fsx.Exists(filepath.Join(dir, "pom.xml")) {
		return "pom.xml", buildMaven, true
	}
	for _, name := range gradleManifests {
		if fsx.Exists(filepath.Join(dir, name)) {
			return name, buildGradle, true
		}
	}
	return "", buildMaven, false
}

// kindOf derives the build kind from a manifest file name.
func kindOf(manifest string) buildKind {
	if filepath.Base(manifest) == "pom.xml" {
		return buildMaven
	}
	return buildGradle
}

// packageManifest returns the repository-relative manifest of pkg. The
// configured manifest wins when it still exists.
func packageManifest(root string, pkg ecosystem.Package) (string, bool) {
	if pkg.Manifest != "" && fsx.Exists(fsx.Join(root, pkg.Manifest)) {
		return pkg.Manifest, true
	}
	name, _, ok := manifestFor(fsx.Join(root, pkg.Path))
	if !ok {
		return "", false
	}
	return path.Join(pkg.Path, name), true
}

// gradleBuildFile returns the Gradle build file name of dir.
func gradleBuildFile(dir string) (string, bool) {
	for _, name := range gradleManifests {
		if fsx.Exists(filepath.Join(dir, name)) {
			return name, true
		}
	}
	return "", false
}

// readGradleProperties returns the key/value pairs of gradle.properties in dir.
func readGradleProperties(dir string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, "gradle.properties"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}
