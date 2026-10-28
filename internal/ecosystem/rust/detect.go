package rust

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type cargoManifest struct {
	Package *struct {
		Name         string         `toml:"name"`
		Version      string         `toml:"version"`
		Dependencies map[string]any `toml:"dependencies"`
	} `toml:"package"`
	Workspace *struct {
		Members []string `toml:"members"`
		Exclude []string `toml:"exclude"`
	} `toml:"workspace"`
}

// Detect finds the root crate and its workspace members.
func (driver) Detect(root string) ([]ecosystem.Package, error) {
	path := filepath.Join(root, "Cargo.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc cargoManifest
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var packages []ecosystem.Package
	if doc.Package != nil && doc.Package.Name != "" {
		packages = append(packages, ecosystem.Package{
			Name:     doc.Package.Name,
			Path:     ".",
			Resolver: "rust",
			Version:  doc.Package.Version,
			Publish:  true,
			Manifest: "Cargo.toml",
			Deps:     deps(doc.Package.Dependencies),
		})
	}
	if doc.Workspace != nil && len(doc.Workspace.Members) > 0 {
		dirs, err := ecosystem.ExpandGlobs(root, doc.Workspace.Members)
		if err != nil {
			return nil, err
		}
		dirs = ecosystem.Exclude(dirs, doc.Workspace.Exclude)
		for _, dir := range dirs {
			if dir == "." {
				continue
			}
			memberPath := filepath.Join(fsx.Join(root, dir), "Cargo.toml")
			memberData, err := os.ReadFile(memberPath)
			if err != nil {
				continue
			}
			var member cargoManifest
			if err := toml.Unmarshal(memberData, &member); err != nil {
				return nil, fmt.Errorf("%s: %w", memberPath, err)
			}
			if member.Package == nil || member.Package.Name == "" {
				continue
			}
			packages = append(packages, ecosystem.Package{
				Name:     member.Package.Name,
				Path:     dir,
				Resolver: "rust",
				Version:  member.Package.Version,
				Publish:  true,
				Manifest: fsx.Join(dir, "Cargo.toml"),
				Deps:     deps(member.Package.Dependencies),
			})
		}
	}
	return packages, nil
}

func deps(table map[string]any) []string {
	out := make([]string, 0, len(table))
	for dep := range table {
		out = append(out, dep)
	}
	return out
}
