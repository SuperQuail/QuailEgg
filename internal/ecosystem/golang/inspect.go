package golang

import (
	"errors"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// Inspect reads the go.mod at the configured package path. Go modules are only
// supported at the repository root.
func (driver) Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	if pkg.Path != "." {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_go_path", pkg.Name, pkg.Path))
	}
	pkgs, err := (driver{}).Detect(root)
	if err != nil {
		return ecosystem.Package{}, err
	}
	if len(pkgs) == 0 {
		path := filepath.Join(root, "go.mod")
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, path))
	}
	return pkgs[0], nil
}

var _ ecosystem.Inspector = driver{}
