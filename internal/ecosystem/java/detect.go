package java

import (
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Detect finds the root project and its Maven modules or Gradle subprojects.
func (driver) Detect(root string) ([]ecosystem.Package, error) {
	if fsx.Exists(filepath.Join(root, "pom.xml")) {
		return detectMaven(root)
	}
	if _, ok := gradleBuildFile(root); ok {
		return detectGradle(root)
	}
	if _, ok := gradleSettingsFile(root); ok {
		return detectGradle(root)
	}
	return nil, nil
}
