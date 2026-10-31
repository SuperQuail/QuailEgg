package java

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Detect finds the root project and its Maven modules or Gradle subprojects.
func (driver) Detect(root string) ([]ecosystem.Package, error) {
	if fsx.Exists(filepath.Join(root, "pom.xml")) {
		return detectMaven(root)
	}
	if manifest, ok := gradleBuildFile(root); ok {
		return detectGradle(root, manifest)
	}
	return nil, nil
}

// detectMaven reads the root pom and every <module> it declares.
func detectMaven(root string) ([]ecosystem.Package, error) {
	doc, err := readPom(filepath.Join(root, "pom.xml"))
	if err != nil {
		return nil, err
	}
	var packages []ecosystem.Package
	if pkg, ok := mavenPackage(".", "pom.xml", doc); ok {
		packages = append(packages, pkg)
	}
	for _, module := range doc.Modules {
		dir := path.Clean(strings.TrimSpace(module))
		if dir == "" || dir == "." || dir == ".." || strings.HasPrefix(dir, "../") {
			continue
		}
		manifest := path.Join(dir, "pom.xml")
		member, err := readPom(fsx.Join(root, manifest))
		if err != nil {
			continue
		}
		if pkg, ok := mavenPackage(dir, manifest, member); ok {
			packages = append(packages, pkg)
		}
	}
	// A parent pom that only aggregates modules is not a package of its own.
	if len(packages) > 1 && packages[0].Version == "" {
		packages = packages[1:]
	}
	return packages, nil
}

// detectGradle reads the root build file and the subprojects from settings.
func detectGradle(root, rootManifest string) ([]ecosystem.Package, error) {
	settings := readGradleSettings(root)
	name := settings.name
	if name == "" {
		name = filepath.Base(root)
	}
	packages := []ecosystem.Package{gradlePackage(root, ".", rootManifest, name)}
	for _, include := range settings.includes {
		dir := strings.TrimPrefix(strings.TrimSpace(include), ":")
		dir = strings.ReplaceAll(dir, ":", "/")
		if dir == "" || dir == ".." || strings.HasPrefix(dir, "../") {
			continue
		}
		manifest, ok := gradleBuildFile(fsx.Join(root, dir))
		if !ok {
			continue
		}
		packages = append(packages, gradlePackage(root, dir, path.Join(dir, manifest), path.Base(dir)))
	}
	// A settings-only root without a version is not a releasable package.
	if len(packages) > 1 && packages[0].Version == "" {
		packages = packages[1:]
	}
	return packages, nil
}
