package java

import (
	"path"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// ScanManifest parses the build file discovered by the bounded repository scan.
func (driver) ScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	dir := fsx.Join(root, pkg.Path)
	name, kind, ok := manifestFor(dir)
	if !ok {
		return ecosystem.ManifestScan{}, nil
	}
	manifest := path.Join(pkg.Path, name)
	if kind == buildGradle {
		return ecosystem.ManifestScan{Package: gradlePackage(root, pkg.Path, manifest, projectName(root, pkg.Path)), OK: true}, nil
	}
	doc, err := readPom(filepath.Join(dir, name))
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	parsed, ok := mavenPackage(pkg.Path, manifest, doc)
	if !ok {
		return ecosystem.ManifestScan{}, nil
	}
	return ecosystem.ManifestScan{Package: parsed, OK: true}, nil
}

var _ ecosystem.ManifestScanner = driver{}
