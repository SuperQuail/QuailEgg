package npm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type packageJSON struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Private      bool              `json:"private"`
	Workspaces   json.RawMessage   `json:"workspaces"`
	Dependencies map[string]string `json:"dependencies"`
}

// Detect finds the root package and its npm/pnpm workspace members.
func (d driver) Detect(root string) ([]ecosystem.Package, error) {
	var packages []ecosystem.Package
	rootFile := filepath.Join(root, "package.json")
	main, ok, err := readPackageJSON(rootFile)
	if err != nil {
		return nil, err
	}
	if !ok {
		dirs, globErr := pnpmMembers(root)
		if globErr != nil {
			return nil, globErr
		}
		if len(dirs) == 0 {
			return nil, nil
		}
		return d.collect(root, dirs)
	}
	packages = append(packages, toPackage(".", "package.json", main))

	dirs, err := npmMembers(root, main)
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		dirs, err = pnpmMembers(root)
		if err != nil {
			return nil, err
		}
	}
	if len(dirs) == 0 {
		return packages, nil
	}
	members, err := d.collect(root, dirs)
	if err != nil {
		return nil, err
	}
	// A workspace root manifest that only aggregates members is not a package.
	if len(members) > 0 && main.Version == "" {
		packages = nil
	}
	packages = append(packages, members...)
	return packages, nil
}

func (d driver) collect(root string, dirs []string) ([]ecosystem.Package, error) {
	var out []ecosystem.Package
	for _, dir := range dirs {
		if dir == "." {
			continue
		}
		manifest := filepath.Join(fsx.Join(root, dir), "package.json")
		pkg, ok, err := readPackageJSON(manifest)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, toPackage(dir, fsx.Join(dir, "package.json"), pkg))
	}
	return out, nil
}

func toPackage(dir, manifest string, pkg packageJSON) ecosystem.Package {
	name := pkg.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	deps := make([]string, 0, len(pkg.Dependencies))
	for dep := range pkg.Dependencies {
		deps = append(deps, dep)
	}
	return ecosystem.Package{
		Name:     name,
		Path:     dir,
		Resolver: "npm",
		Version:  pkg.Version,
		Publish:  !pkg.Private,
		Manifest: manifest,
		Deps:     deps,
	}
}

func readPackageJSON(path string) (packageJSON, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return packageJSON{}, false, nil
		}
		return packageJSON{}, false, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSON{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return pkg, true, nil
}

func npmMembers(root string, pkg packageJSON) ([]string, error) {
	if len(pkg.Workspaces) == 0 {
		return nil, nil
	}
	var patterns []string
	if err := json.Unmarshal(pkg.Workspaces, &patterns); err != nil {
		var obj struct {
			Packages []string `json:"packages"`
		}
		if err2 := json.Unmarshal(pkg.Workspaces, &obj); err2 != nil {
			return nil, fmt.Errorf("package.json workspaces: %w", err)
		}
		patterns = obj.Packages
	}
	return ecosystem.ExpandGlobs(root, patterns)
}

func pnpmMembers(root string) ([]string, error) {
	path := filepath.Join(root, "pnpm-workspace.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Packages []string `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ecosystem.ExpandGlobs(root, doc.Packages)
}
