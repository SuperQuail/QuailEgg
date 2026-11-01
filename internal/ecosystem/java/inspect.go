package java

import (
	"errors"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Inspect reads the build file at the configured package path.
func (driver) Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, fsx.Join(root, pkg.Path)))
	}
	if kindOf(manifest) == buildMaven {
		return mavenInspect(root, pkg)
	}
	return gradleInspect(root, pkg)
}

var _ ecosystem.Inspector = driver{}
