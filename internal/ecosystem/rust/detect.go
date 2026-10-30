package rust

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// cargoPackage is one [package] table. Version and Publish stay untyped because
// Cargo accepts both a literal value and workspace inheritance for them.
type cargoPackage struct {
	Name    string `toml:"name"`
	Version any    `toml:"version"`
	Publish any    `toml:"publish"`
}

type cargoManifest struct {
	Package *cargoPackage `toml:"package"`
	// Dependencies live next to [package], not inside it.
	Dependencies      map[string]any `toml:"dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
	Workspace         *struct {
		Members []string `toml:"members"`
		Exclude []string `toml:"exclude"`
		Package *struct {
			Version string `toml:"version"`
		} `toml:"package"`
	} `toml:"workspace"`
}

// Detect finds the root crate and its workspace members. Members that inherit
// their version from [workspace.package] are resolved against the root
// manifest, and Cargo's publish = false is carried over.
func (driver) Detect(root string) ([]ecosystem.Package, error) {
	rootManifest := filepath.Join(root, "Cargo.toml")
	data, err := os.ReadFile(rootManifest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc cargoManifest
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", rootManifest, err)
	}
	workspaceVersion := ""
	if doc.Workspace != nil && doc.Workspace.Package != nil {
		workspaceVersion = strings.TrimSpace(doc.Workspace.Package.Version)
	}

	var packages []ecosystem.Package
	if doc.Package != nil && doc.Package.Name != "" {
		pkg, err := detectPackage(".", doc.Package, workspaceVersion, deps(doc.Dependencies, doc.BuildDependencies))
		if err != nil {
			return nil, err
		}
		packages = append(packages, pkg)
	}
	if doc.Workspace == nil || len(doc.Workspace.Members) == 0 {
		return packages, nil
	}
	dirs, err := ecosystem.ExpandGlobs(root, doc.Workspace.Members)
	if err != nil {
		return nil, err
	}
	dirs = ecosystem.Exclude(dirs, doc.Workspace.Exclude)
	for _, dir := range dirs {
		if dir == "." {
			continue
		}
		memberManifest := filepath.Join(fsx.Join(root, dir), "Cargo.toml")
		memberData, err := os.ReadFile(memberManifest)
		if err != nil {
			continue
		}
		var member cargoManifest
		if err := toml.Unmarshal(memberData, &member); err != nil {
			return nil, fmt.Errorf("%s: %w", memberManifest, err)
		}
		if member.Package == nil || member.Package.Name == "" {
			continue
		}
		pkg, err := detectPackage(dir, member.Package, workspaceVersion, deps(member.Dependencies, member.BuildDependencies))
		if err != nil {
			return nil, err
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// detectPackage converts one [package] table into an ecosystem package.
func detectPackage(dir string, pkg *cargoPackage, workspaceVersion string, dependencies []string) (ecosystem.Package, error) {
	manifest := path.Join(dir, "Cargo.toml")
	version, inherited, err := packageVersion(pkg.Version, workspaceVersion, manifest)
	if err != nil {
		return ecosystem.Package{}, err
	}
	publish, err := packagePublish(pkg.Publish, manifest)
	if err != nil {
		return ecosystem.Package{}, err
	}
	return ecosystem.Package{
		Name:      pkg.Name,
		Path:      dir,
		Resolver:  "rust",
		Version:   version,
		Inherited: inherited,
		Publish:   publish,
		Manifest:  manifest,
		Deps:      dependencies,
	}, nil
}

// packageVersion resolves a [package].version value, following Cargo's
// version.workspace = true inheritance to the workspace root.
func packageVersion(raw any, workspaceVersion, manifest string) (string, bool, error) {
	switch value := raw.(type) {
	case nil:
		return "", false, nil
	case string:
		return strings.TrimSpace(value), false, nil
	case map[string]any:
		if inherit, ok := value["workspace"].(bool); ok && inherit {
			if workspaceVersion == "" {
				return "", false, errors.New(i18n.Tf("rust.err.workspace_version_missing", manifest))
			}
			return workspaceVersion, true, nil
		}
	}
	return "", false, errors.New(i18n.Tf("rust.err.version_form", manifest))
}

// packagePublish resolves a [package].publish value. qegg publishes to
// crates.io only, so a package restricted to other registries is not published.
func packagePublish(raw any, manifest string) (bool, error) {
	switch value := raw.(type) {
	case nil:
		return true, nil
	case bool:
		return value, nil
	case []any:
		for _, item := range value {
			if name, ok := item.(string); ok && strings.EqualFold(strings.TrimSpace(name), "crates-io") {
				return true, nil
			}
		}
		return false, nil
	}
	return false, errors.New(i18n.Tf("rust.err.publish_form", manifest))
}

// deps lists the dependency names of a package, resolving inline table renames
// so the release ordering picks the real package names.
func deps(tables ...map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, table := range tables {
		for key, value := range table {
			name := key
			if inline, ok := value.(map[string]any); ok {
				if renamed, ok := inline["package"].(string); ok && strings.TrimSpace(renamed) != "" {
					name = strings.TrimSpace(renamed)
				}
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
