package npm

import (
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// ScanManifest parses a package.json discovered by the bounded repository scan.
// It reuses the workspace detection rules, so a manifest without a version is
// still returned and left for the caller to reject.
func (driver) ScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	manifest := filepath.Join(fsx.Join(root, pkg.Path), "package.json")
	parsed, ok, err := readPackageJSON(manifest)
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	if !ok {
		return ecosystem.ManifestScan{}, nil
	}
	return ecosystem.ManifestScan{Package: toPackage(pkg.Path, pkg.Manifest, parsed), OK: true}, nil
}

var _ ecosystem.ManifestScanner = driver{}
