package java

import (
	"errors"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// SetVersion writes next into the project version of the build file. A Maven
// module that inherits its parent version is skipped: SyncVersions updates the
// parent reference once the parent itself is bumped.
func (driver) SetVersion(root string, pkg ecosystem.Package, next string) error {
	if pkg.Inherited {
		return nil
	}
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, fsx.Join(root, pkg.Path)))
	}
	if kindOf(manifest) == buildGradle {
		return setGradleVersion(root, pkg, next)
	}
	return setMavenVersion(root, pkg, next)
}
