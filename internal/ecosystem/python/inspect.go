package python

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Inspect reads the pyproject.toml at the configured package path.
func (driver) Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	manifest := filepath.Join(fsx.Join(root, pkg.Path), "pyproject.toml")
	doc, err := readPyProject(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, manifest))
		}
		return ecosystem.Package{}, err
	}
	inspected, ok, err := toPackage(root, pkg.Path, doc)
	if err != nil {
		return ecosystem.Package{}, err
	}
	if !ok {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_package", manifest))
	}
	return inspected, nil
}

var _ ecosystem.Inspector = driver{}
