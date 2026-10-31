package java

import (
	"context"
	"os"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// SyncVersions updates the <parent> reference of Maven modules that inherit
// their version, so a bumped parent version stays consistent. Gradle
// subprojects share the property that SetVersion already rewrote.
func (driver) SyncVersions(ctx context.Context, run execx.Runner, root string, pkgs []ecosystem.Package) ([]string, error) {
	var changed []string
	for _, pkg := range pkgs {
		if !pkg.Inherited || pkg.Version == "" {
			continue
		}
		manifest, ok := packageManifest(root, pkg)
		if !ok || kindOf(manifest) != buildMaven {
			continue
		}
		file := fsx.Join(root, manifest)
		doc, err := readPom(file)
		if err != nil || doc.Parent == nil {
			continue
		}
		if strings.TrimSpace(doc.Parent.Version) == pkg.Version {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		updated, ok := setElementText(string(data), []string{"project", "parent", "version"}, pkg.Version)
		if !ok {
			continue
		}
		if err := fsx.WriteFileAtomic(file, []byte(updated), 0o644); err != nil {
			return nil, err
		}
		changed = append(changed, fsx.Rel(root, file))
	}
	return changed, nil
}

var _ ecosystem.VersionSyncer = driver{}
