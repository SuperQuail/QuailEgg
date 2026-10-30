package npm

import (
	"errors"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Inspect reads the package.json at the configured package path.
func (driver) Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	manifest := filepath.Join(fsx.Join(root, pkg.Path), "package.json")
	parsed, ok, err := readPackageJSON(manifest)
	if err != nil {
		return ecosystem.Package{}, err
	}
	if !ok {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, manifest))
	}
	return toPackage(pkg.Path, fsx.Join(pkg.Path, "package.json"), parsed), nil
}

var _ ecosystem.Inspector = driver{}
