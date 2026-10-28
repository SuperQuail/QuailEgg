package python

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type pyProject struct {
	Project *struct {
		Name         string   `toml:"name"`
		Version      string   `toml:"version"`
		Dynamic      []string `toml:"dynamic"`
		Dependencies []string `toml:"dependencies"`
	} `toml:"project"`
	Tool *struct {
		Poetry *struct {
			Name         string         `toml:"name"`
			Version      string         `toml:"version"`
			Dependencies map[string]any `toml:"dependencies"`
		} `toml:"poetry"`
		Hatch *struct {
			Version *struct {
				Path string `toml:"path"`
			} `toml:"version"`
		} `toml:"hatch"`
		Setuptools *struct {
			Dynamic *struct {
				Version *struct {
					Attr string `toml:"attr"`
					File string `toml:"file"`
				} `toml:"version"`
			} `toml:"dynamic"`
		} `toml:"setuptools"`
		Uv *struct {
			Workspace *struct {
				Members []string `toml:"members"`
				Exclude []string `toml:"exclude"`
			} `toml:"workspace"`
		} `toml:"uv"`
	} `toml:"tool"`
}

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func (d driver) Detect(root string) ([]ecosystem.Package, error) {
	rootManifest := filepath.Join(root, "pyproject.toml")
	if _, err := os.Stat(rootManifest); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	doc, err := readPyProject(rootManifest)
	if err != nil {
		return nil, err
	}
	packages := []ecosystem.Package{}
	rootPkg, ok, err := toPackage(root, ".", doc)
	if err != nil {
		return nil, err
	}
	if ok {
		packages = append(packages, rootPkg)
	}
	if doc.Tool != nil && doc.Tool.Uv != nil && doc.Tool.Uv.Workspace != nil {
		dirs, err := ecosystem.ExpandGlobs(root, doc.Tool.Uv.Workspace.Members)
		if err != nil {
			return nil, err
		}
		dirs = ecosystem.Exclude(dirs, doc.Tool.Uv.Workspace.Exclude)
		for _, dir := range dirs {
			if dir == "." {
				continue
			}
			manifest := filepath.Join(fsx.Join(root, dir), "pyproject.toml")
			memberDoc, err := readPyProject(manifest)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			pkg, ok, err := toPackage(root, dir, memberDoc)
			if err != nil {
				return nil, err
			}
			if ok {
				packages = append(packages, pkg)
			}
		}
	}
	return packages, nil
}

func readPyProject(path string) (*pyProject, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc pyProject
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &doc, nil
}

func toPackage(root, dir string, doc *pyProject) (ecosystem.Package, bool, error) {
	name := ""
	if doc.Project != nil {
		name = doc.Project.Name
	}
	var poetryVersion string
	if doc.Tool != nil && doc.Tool.Poetry != nil {
		if name == "" {
			name = doc.Tool.Poetry.Name
		}
		poetryVersion = doc.Tool.Poetry.Version
	}
	if name == "" {
		return ecosystem.Package{}, false, nil
	}
	version := ""
	manifest := fsx.Join(dir, "pyproject.toml")
	path := filepath.Join(fsx.Join(root, dir), "pyproject.toml")
	switch {
	case doc.Project != nil && doc.Project.Version != "":
		version = doc.Project.Version
	case poetryVersion != "":
		version = poetryVersion
	case doc.Project != nil && containsString(doc.Project.Dynamic, "version"):
		resolved, err := dynamicVersion(path)
		if err != nil {
			return ecosystem.Package{}, false, fmt.Errorf("%s: %w", path, err)
		}
		version = resolved
	}
	if version == "" {
		return ecosystem.Package{}, false, fmt.Errorf("%s: cannot determine package version", path)
	}
	if !releaseVersion.MatchString(version) {
		return ecosystem.Package{}, false, fmt.Errorf("%s: version %q is not X.Y.Z (PEP 440 pre-release versions are not supported in v0.1)", path, version)
	}
	deps := []string{}
	if doc.Project != nil {
		deps = append(deps, doc.Project.Dependencies...)
	}
	return ecosystem.Package{
		Name:     normalizeName(name),
		Path:     dir,
		Resolver: "python",
		Version:  version,
		Publish:  true,
		Manifest: manifest,
		Deps:     deps,
	}, true, nil
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// normalizeName applies the PyPI name normalisation.
func normalizeName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		switch r {
		case '-', '_', '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			b.WriteRune(r)
			lastDash = false
		}
	}
	return strings.Trim(b.String(), "-")
}
