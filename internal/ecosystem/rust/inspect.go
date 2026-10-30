package rust

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Inspect reads the Cargo.toml at the configured package path. A member that
// inherits version.workspace resolves it through the repository root manifest.
func (driver) Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	manifest := filepath.Join(fsx.Join(root, pkg.Path), "Cargo.toml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, manifest))
		}
		return ecosystem.Package{}, err
	}
	var doc cargoManifest
	if err := toml.Unmarshal(data, &doc); err != nil {
		return ecosystem.Package{}, fmt.Errorf("%s: %w", manifest, err)
	}
	if doc.Package == nil || doc.Package.Name == "" {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_package", manifest))
	}
	workspaceVersion, err := inspectWorkspaceVersion(root, pkg.Path, &doc)
	if err != nil {
		return ecosystem.Package{}, err
	}
	return detectPackage(pkg.Path, doc.Package, workspaceVersion, deps(doc.Dependencies, doc.BuildDependencies))
}

// inspectWorkspaceVersion resolves [workspace.package].version from the root
// manifest. When the inspected manifest is the root itself the parsed document
// already carries it.
func inspectWorkspaceVersion(root, dir string, member *cargoManifest) (string, error) {
	if dir == "." {
		return workspaceVersionOf(member), nil
	}
	data, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var rootDoc cargoManifest
	if err := toml.Unmarshal(data, &rootDoc); err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Join(root, "Cargo.toml"), err)
	}
	return workspaceVersionOf(&rootDoc), nil
}

func workspaceVersionOf(doc *cargoManifest) string {
	if doc.Workspace == nil || doc.Workspace.Package == nil {
		return ""
	}
	return doc.Workspace.Package.Version
}

var _ ecosystem.Inspector = driver{}
