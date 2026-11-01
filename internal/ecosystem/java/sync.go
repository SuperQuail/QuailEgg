package java

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// SyncVersions keeps internal references in sync after the manifests were
// bumped. Maven and Gradle packages are handled separately.
func (driver) SyncVersions(ctx context.Context, run execx.Runner, root string, pkgs []ecosystem.Package) ([]string, error) {
	var mavenPkgs []ecosystem.Package
	var gradlePkgs []ecosystem.Package
	for _, pkg := range pkgs {
		manifest, ok := packageManifest(root, pkg)
		if !ok {
			continue
		}
		if kindOf(manifest) == buildMaven {
			mavenPkgs = append(mavenPkgs, pkg)
			continue
		}
		gradlePkgs = append(gradlePkgs, pkg)
	}
	var changed []string
	if len(mavenPkgs) > 0 {
		paths, err := mavenSync(root, mavenPkgs)
		if err != nil {
			return nil, err
		}
		changed = append(changed, paths...)
	}
	if len(gradlePkgs) > 0 {
		paths, err := gradleSync(root, gradlePkgs)
		if err != nil {
			return nil, err
		}
		changed = append(changed, paths...)
	}
	return uniqueSorted(changed), nil
}

var _ ecosystem.VersionSyncer = driver{}
