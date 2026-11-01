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

// Build kinds of a Java package.
const (
	buildMaven buildKind = iota
	buildGradle
)

// Gradle file names in the order Gradle tries them: the Groovy build file wins
// when both exist. The same order applies to settings files.
var (
	gradleBuildFiles    = []string{"build.gradle", "build.gradle.kts"}
	gradleSettingsFiles = []string{"settings.gradle", "settings.gradle.kts"}
)

// manifestFor returns the build file of dir and its kind. Maven wins when a
// directory carries both build systems.
func manifestFor(dir string) (string, buildKind, bool) {
	if fsx.Exists(filepath.Join(dir, "pom.xml")) {
		return "pom.xml", buildMaven, true
	}
	if name, ok := gradleBuildFile(dir); ok {
		return name, buildGradle, true
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
	for _, name := range gradleBuildFiles {
		if fsx.Exists(filepath.Join(dir, name)) {
			return name, true
		}
	}
	return "", false
}

// gradleSettingsFile returns the Gradle settings file name of dir.
func gradleSettingsFile(dir string) (string, bool) {
	for _, name := range gradleSettingsFiles {
		if fsx.Exists(filepath.Join(dir, name)) {
			return name, true
		}
	}
	return "", false
}

// readGradleProperties returns the project properties declared in the root
// gradle.properties. Gradle only reads the root project file, not subproject
// copies, so qegg does the same.
func readGradleProperties(root string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(root, "gradle.properties"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := gradlePropertyLine(line)
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}

// gradlePropertyLine parses one gradle.properties line into key and value.
func gradlePropertyLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
		return "", "", false
	}
	sep := strings.IndexAny(line, "=:")
	if sep < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:sep])
	value := strings.TrimSpace(line[sep+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// writeGradleProperty rewrites one root gradle.properties entry in place and
// returns its repository-relative path. Every other byte, including line
// endings, is preserved.
func writeGradleProperty(root, key, value string) (string, error) {
	file := filepath.Join(root, "gradle.properties")
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	lines := strings.SplitAfter(string(data), "\n")
	for i, line := range lines {
		content := strings.TrimSuffix(line, "\n")
		ending := "\n"
		if strings.HasSuffix(content, "\r") {
			content = strings.TrimSuffix(content, "\r")
			ending = "\r\n"
		}
		raw := content
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		sep := strings.IndexAny(raw, "=:")
		if sep < 0 || strings.TrimSpace(raw[:sep]) != key {
			continue
		}
		prefixEnd := sep + 1
		for prefixEnd < len(raw) && (raw[prefixEnd] == ' ' || raw[prefixEnd] == '\t') {
			prefixEnd++
		}
		next := raw[:prefixEnd] + value
		if next == raw {
			return "gradle.properties", nil
		}
		lines[i] = next + ending
		if err := fsx.WriteFileAtomic(file, []byte(strings.Join(lines, "")), 0o644); err != nil {
			return "", err
		}
		return "gradle.properties", nil
	}
	return "", nil
}

// rootBuildKind returns the build system declared by the repository root.
func rootBuildKind(root string) (buildKind, bool) {
	if fsx.Exists(filepath.Join(root, "pom.xml")) {
		return buildMaven, true
	}
	if _, ok := gradleBuildFile(root); ok {
		return buildGradle, true
	}
	if _, ok := gradleSettingsFile(root); ok {
		return buildGradle, true
	}
	return buildMaven, false
}

// foreignBuildSystem reports whether manifest belongs to another build system
// than the repository root. A Maven pom nested inside a Gradle repository (a
// test fixture, for example) is not a package qegg should register by default.
func foreignBuildSystem(root, manifest string) bool {
	if manifest == "" {
		return false
	}
	rootKind, ok := rootBuildKind(root)
	if !ok {
		return false
	}
	kind := kindOf(manifest)
	if kind == rootKind {
		return false
	}
	dir := path.Dir(path.Dir(manifest))
	for dir != "." && dir != "/" && dir != "" {
		switch kind {
		case buildMaven:
			if fsx.Exists(filepath.Join(root, filepath.FromSlash(dir), "pom.xml")) {
				return false
			}
		case buildGradle:
			if _, ok := gradleBuildFile(fsx.Join(root, dir)); ok {
				return false
			}
			if _, ok := gradleSettingsFile(fsx.Join(root, dir)); ok {
				return false
			}
		}
		dir = path.Dir(dir)
	}
	return true
}

// findWrapper walks from dir up to root looking for one of names and returns
// its absolute path. It lets a subproject reuse the wrapper of the repository.
func findWrapper(root, dir string, names ...string) (string, bool) {
	rootDir := filepath.Clean(fsx.Join(root, "."))
	cur := filepath.Clean(fsx.Join(root, dir))
	for {
		for _, name := range names {
			candidate := filepath.Join(cur, name)
			if fsx.Exists(candidate) {
				return candidate, true
			}
		}
		if cur == rootDir {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur || !strings.HasPrefix(strings.ToLower(parent), strings.ToLower(rootDir)) {
			break
		}
		cur = parent
	}
	return "", false
}

// shellQuote quotes one argument for the POSIX shell.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// windowsQuote quotes one command line argument for cmd.exe.
func windowsQuote(value string) string {
	return "\"" + value + "\""
}
