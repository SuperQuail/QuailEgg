package java

import (
	"path"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// ScanManifest parses the build file discovered by the bounded repository scan.
func (driver) ScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	manifest := pkg.Manifest
	if manifest == "" {
		name, _, ok := manifestFor(fsx.Join(root, pkg.Path))
		if !ok {
			return ecosystem.ManifestScan{}, nil
		}
		manifest = path.Join(pkg.Path, name)
	}
	if kindOf(manifest) == buildMaven {
		return mavenScanManifest(root, pkg)
	}
	return gradleScanManifest(root, pkg)
}

var _ ecosystem.ManifestScanner = driver{}
