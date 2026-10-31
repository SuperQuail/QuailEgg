package rust

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// ScanManifest parses a Cargo.toml discovered by the bounded repository scan.
// Inherited versions are resolved against the workspace manifest at the
// repository root, which is the only workspace qegg supports.
func (driver) ScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	manifest := filepath.Join(fsx.Join(root, pkg.Path), "Cargo.toml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return ecosystem.ManifestScan{}, nil
		}
		return ecosystem.ManifestScan{}, err
	}
	var doc cargoManifest
	if err := toml.Unmarshal(data, &doc); err != nil {
		return ecosystem.ManifestScan{}, err
	}
	if doc.Package == nil || doc.Package.Name == "" {
		return ecosystem.ManifestScan{}, nil
	}
	base := ecosystem.Package{Path: pkg.Path, Manifest: pkg.Manifest, Resolver: pkg.Resolver}
	workspaceVersion := ""
	if pkg.Path == "." {
		workspaceVersion = workspaceVersionOf(&doc)
	} else if workspaceInherited(doc.Package.Version) {
		if doc.Workspace != nil || nestedWorkspace(root, pkg.Path) {
			return ecosystem.ManifestScan{
				Package: base,
				Reason:  ecosystem.ScanReasonNestedWorkspace,
				Detail:  i18n.Tf("scan.detail.nested_workspace", pkg.Manifest),
			}, nil
		}
		workspaceVersion, err = rootWorkspaceVersion(root)
		if err != nil {
			return ecosystem.ManifestScan{}, err
		}
		if workspaceVersion == "" {
			return ecosystem.ManifestScan{
				Package: base,
				Reason:  ecosystem.ScanReasonWorkspaceVersion,
				Detail:  i18n.Tf("rust.err.workspace_version_missing", pkg.Manifest),
			}, nil
		}
	}
	detected, err := detectPackage(pkg.Path, doc.Package, workspaceVersion, deps(doc.Dependencies, doc.BuildDependencies))
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	return ecosystem.ManifestScan{Package: detected, OK: true}, nil
}

// workspaceInherited reports whether a [package].version value inherits the
// workspace version.
func workspaceInherited(raw any) bool {
	value, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	inherit, _ := value["workspace"].(bool)
	return inherit
}

// nestedWorkspace reports whether an ancestor below the repository root
// declares its own Cargo workspace.
func nestedWorkspace(root, dir string) bool {
	parent := path.Dir(dir)
	for parent != "." && parent != "/" && parent != "" {
		data, err := os.ReadFile(filepath.Join(fsx.Join(root, parent), "Cargo.toml"))
		if err == nil && declaresWorkspace(data) {
			return true
		}
		parent = path.Dir(parent)
	}
	return false
}

// declaresWorkspace reports whether a manifest carries a [workspace] table.
func declaresWorkspace(data []byte) bool {
	var doc struct {
		Workspace *struct{} `toml:"workspace"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return doc.Workspace != nil
}

// rootWorkspaceVersion returns [workspace.package].version from the repository
// root manifest, or an empty string when the workspace does not set one.
func rootWorkspaceVersion(root string) (string, error) {
	manifest := filepath.Join(root, "Cargo.toml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var doc cargoManifest
	if err := toml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", manifest, err)
	}
	return workspaceVersionOf(&doc), nil
}

var _ ecosystem.ManifestScanner = driver{}
