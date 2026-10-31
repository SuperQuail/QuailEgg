package java

import (
	"errors"
	"path"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Inspect reads the build file at the configured package path.
func (driver) Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	dir := fsx.Join(root, pkg.Path)
	name, kind, ok := manifestFor(dir)
	if !ok {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, filepath.Join(dir, "pom.xml")))
	}
	manifest := path.Join(pkg.Path, name)
	if kind == buildGradle {
		return gradlePackage(root, pkg.Path, manifest, projectName(root, pkg.Path)), nil
	}
	doc, err := readPom(filepath.Join(dir, name))
	if err != nil {
		return ecosystem.Package{}, err
	}
	inspected, ok := mavenPackage(pkg.Path, manifest, doc)
	if !ok {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_package", manifest))
	}
	return inspected, nil
}

var _ ecosystem.Inspector = driver{}
