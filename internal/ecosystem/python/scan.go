package python

import (
	"os"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// ScanManifest parses a pyproject.toml discovered by the bounded repository scan.
func (driver) ScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	manifest := filepath.Join(fsx.Join(root, pkg.Path), "pyproject.toml")
	doc, err := readPyProject(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return ecosystem.ManifestScan{}, nil
		}
		return ecosystem.ManifestScan{}, err
	}
	parsed, ok, err := toPackage(root, pkg.Path, doc)
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	if !ok {
		return ecosystem.ManifestScan{}, nil
	}
	return ecosystem.ManifestScan{Package: parsed, OK: true}, nil
}

var _ ecosystem.ManifestScanner = driver{}
