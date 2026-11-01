package java

import (
	"errors"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// SetVersion writes next into the version source of the build file.
func (driver) SetVersion(root string, pkg ecosystem.Package, next string) error {
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, fsx.Join(root, pkg.Path)))
	}
	if kindOf(manifest) == buildMaven {
		return mavenSetVersion(root, pkg, next)
	}
	return gradleSetVersion(root, pkg, next)
}

// VersionPaths reports files written by SetVersion outside the manifest.
func (driver) VersionPaths(root string, pkg ecosystem.Package, next string) ([]string, error) {
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return nil, nil
	}
	if kindOf(manifest) == buildMaven {
		return nil, nil
	}
	return gradleVersionPaths(root, pkg, next)
}

var (
	_ ecosystem.VersionPathReporter = driver{}
)
