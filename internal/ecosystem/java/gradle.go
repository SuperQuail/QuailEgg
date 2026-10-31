package java

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// gradleSettings is the project layout declared by settings.gradle(.kts).
type gradleSettings struct {
	name     string
	includes []string
}

var (
	settingsNameLine    = regexp.MustCompile(`(?m)^\s*rootProject\.name\s*=\s*["']([^"']+)["']`)
	settingsIncludeLine = regexp.MustCompile(`(?m)^\s*include\b[ \t]*\(?([^\n]*)`)
	gradleVersionLine   = regexp.MustCompile(`(?m)^[ \t]*version[ \t]*(?:=[ \t]*)?("[^"]*"|'[^']*'|[A-Za-z_$][\w.$]*)`)
	gradlePublishLine   = regexp.MustCompile(`(?m)(maven-publish|^[ \t]*publishing[ \t]*\{)`)
)

// readGradleSettings reads the root project name and the included subprojects.
func readGradleSettings(root string) gradleSettings {
	settings := gradleSettings{}
	for _, name := range []string{"settings.gradle.kts", "settings.gradle"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		text := string(data)
		if m := settingsNameLine.FindStringSubmatch(text); m != nil {
			settings.name = strings.TrimSpace(m[1])
		}
		for _, m := range settingsIncludeLine.FindAllStringSubmatch(text, -1) {
			settings.includes = append(settings.includes, gradleIncludeValues(m[1])...)
		}
		break
	}
	return settings
}

// gradleIncludeValues extracts the project paths of one include statement.
func gradleIncludeValues(rest string) []string {
	var out []string
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimLeft(part, "({ \t")
		part = strings.TrimRight(part, "}){ \t\r")
		part = strings.Trim(part, `"'`)
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// gradleVersion reads the project version, resolving a gradle.properties
// reference such as version = mod_version.
func gradleVersion(root, dir, manifest string) string {
	data, err := os.ReadFile(fsx.Join(root, manifest))
	if err != nil {
		return ""
	}
	m := gradleVersionLine.FindStringSubmatch(string(data))
	if m == nil {
		return ""
	}
	value := strings.TrimSpace(m[1])
	if literal, ok := unquoteGradle(value); ok {
		return literal
	}
	name := strings.TrimPrefix(value, "project.")
	if name == "" {
		return ""
	}
	if version := readGradleProperties(fsx.Join(root, dir))[name]; version != "" {
		return version
	}
	return readGradleProperties(root)[name]
}

// gradlePublishes reports whether the build file configures a Maven
// publication, which is what makes a project deployable.
func gradlePublishes(root, manifest string) bool {
	data, err := os.ReadFile(fsx.Join(root, manifest))
	if err != nil {
		return false
	}
	return gradlePublishLine.MatchString(string(data))
}

// unquoteGradle removes the quotes of a Gradle string literal.
func unquoteGradle(value string) (string, bool) {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1], true
	}
	return "", false
}

// gradlePackage converts one build file into an ecosystem package.
func gradlePackage(root, dir, manifest, name string) ecosystem.Package {
	return ecosystem.Package{
		Name:     name,
		Path:     dir,
		Resolver: "java",
		Version:  gradleVersion(root, dir, manifest),
		Publish:  gradlePublishes(root, manifest),
		Manifest: manifest,
	}
}

// setGradleVersion rewrites the version literal, or the gradle.properties
// entry the version line points at.
func setGradleVersion(root string, pkg ecosystem.Package, next string) error {
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, fsx.Join(root, pkg.Path)))
	}
	file := fsx.Join(root, manifest)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	text := string(data)
	loc := gradleVersionLine.FindStringSubmatchIndex(text)
	if loc == nil {
		return errors.New(i18n.Tf("java.err.version_missing", file))
	}
	value := text[loc[2]:loc[3]]
	if _, ok := unquoteGradle(value); ok {
		quote := value[:1]
		updated := text[:loc[2]] + quote + next + quote + text[loc[3]:]
		return fsx.WriteFileAtomic(file, []byte(updated), 0o644)
	}
	name := strings.TrimPrefix(value, "project.")
	if name == "" {
		return errors.New(i18n.Tf("java.err.version_form", value, file))
	}
	return writeGradleProperty(root, pkg.Path, name, next)
}

// writeGradleProperty updates one gradle.properties entry, preferring the
// package directory and falling back to the repository root.
func writeGradleProperty(root, dir, name, next string) error {
	for _, candidate := range []string{fsx.Join(root, dir), root} {
		file := filepath.Join(candidate, "gradle.properties")
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			key := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
			if key != name {
				continue
			}
			lines[i] = name + "=" + next
			return fsx.WriteFileAtomic(file, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return errors.New(i18n.Tf("java.err.gradle_property", fsx.Join(root, dir), name))
}

// projectName resolves the name of one Gradle project. A directory with its
// own settings file is a build root, so its rootProject.name wins.
func projectName(root, dir string) string {
	if name := readGradleSettings(fsx.Join(root, dir)).name; name != "" {
		return name
	}
	if dir == "." || dir == "" {
		return filepath.Base(root)
	}
	return path.Base(dir)
}
