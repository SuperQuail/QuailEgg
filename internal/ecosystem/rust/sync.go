package rust

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// inlineDependency matches one single-line inline table in a dependency table.
var inlineDependency = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+|"[^"]+")\s*=\s*\{([^}]*)\}(\s*)$`)

// pathValue and versionValue pull the two fields back out of that table.
var (
	pathValue    = regexp.MustCompile(`\bpath\s*=\s*"([^"]*)"`)
	versionValue = regexp.MustCompile(`\bversion\s*=\s*"([^"]*)"`)
)

// SyncVersions writes the Cargo workspace version once, updates sibling path
// dependencies to the new versions and refreshes Cargo.lock. Cargo stores one
// version per workspace, so inherited members share the version they are given.
func (driver) SyncVersions(ctx context.Context, run execx.Runner, root string, pkgs []ecosystem.Package) ([]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	detected, err := (driver{}).Detect(root)
	if err != nil {
		return nil, err
	}
	versions := make(map[string]string, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.Version != "" {
			versions[pkg.Name] = pkg.Version
		}
	}
	changed := map[string]bool{}
	rootManifest := filepath.Join(root, "Cargo.toml")
	if err := writeWorkspaceVersion(rootManifest, pkgs, changed); err != nil {
		return nil, err
	}
	byDir := make(map[string]ecosystem.Package, len(detected))
	for _, pkg := range detected {
		byDir[filepath.Clean(fsx.Join(root, pkg.Path))] = pkg
	}
	manifests := []string{rootManifest}
	for _, pkg := range detected {
		manifests = append(manifests, filepath.Join(root, filepath.FromSlash(pkg.Manifest)))
	}
	for _, manifest := range uniquePaths(manifests) {
		updated, err := rewriteDependencyVersions(manifest, byDir, versions)
		if err != nil {
			return nil, err
		}
		if updated {
			changed[fsx.Rel(root, manifest)] = true
		}
	}
	if _, err := os.Stat(filepath.Join(root, "Cargo.lock")); err == nil {
		if _, err := run.Run(ctx, root, "cargo", "update", "--workspace"); err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T("rust.err.cargo_update"), err)
		}
		changed["Cargo.lock"] = true
	}
	return sortedKeys(changed), nil
}

// writeWorkspaceVersion updates [workspace.package].version when members
// inherit their version from it.
func writeWorkspaceVersion(manifest string, pkgs []ecosystem.Package, changed map[string]bool) error {
	var inherited []ecosystem.Package
	for _, pkg := range pkgs {
		if pkg.Inherited {
			inherited = append(inherited, pkg)
		}
	}
	if len(inherited) == 0 {
		return nil
	}
	next := inherited[0].Version
	for _, pkg := range inherited[1:] {
		if pkg.Version != next {
			return errors.New(i18n.Tf("rust.err.workspace_version_conflict", inherited[0].Name, pkg.Name))
		}
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inSection := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inSection = trimmed == "[workspace.package]"
			continue
		}
		if !inSection || !versionLine.MatchString(line) {
			continue
		}
		updated := quotedValue.ReplaceAllStringFunc(line, func(string) string {
			return strconv.Quote(next)
		})
		if updated == line {
			return nil
		}
		lines[i] = updated
		changed["Cargo.toml"] = true
		return os.WriteFile(manifest, []byte(strings.Join(lines, "\n")), 0o644)
	}
	return errors.New(i18n.T("rust.err.workspace_package_missing"))
}

// rewriteDependencyVersions updates the version of every path dependency that
// points at a workspace package qegg knows about.
func rewriteDependencyVersions(manifest string, byDir map[string]ecosystem.Package, versions map[string]string) (bool, error) {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	section := ""
	dirty := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.Trim(trimmed, "[]")
			continue
		}
		if !dependencySection(section) {
			continue
		}
		match := inlineDependency.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		pathMatch := pathValue.FindStringSubmatch(match[3])
		if pathMatch == nil {
			continue
		}
		target, ok := byDir[filepath.Clean(fsx.Join(filepath.Dir(manifest), filepath.FromSlash(pathMatch[1])))]
		if !ok {
			continue
		}
		next, ok := versions[target.Name]
		if !ok {
			continue
		}
		updated := setInlineVersion(match, match[3], next)
		if updated != line {
			lines[i] = updated
			dirty = true
		}
	}
	if !dirty {
		return false, nil
	}
	if err := os.WriteFile(manifest, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// dependencySection reports whether a section may pin a sibling package.
func dependencySection(name string) bool {
	switch name {
	case "dependencies", "build-dependencies", "workspace.dependencies":
		return true
	}
	if strings.HasPrefix(name, "target.") {
		return strings.HasSuffix(name, ".dependencies") || strings.HasSuffix(name, ".build-dependencies")
	}
	return false
}

// setInlineVersion rewrites or adds version = "x" inside one inline table.
func setInlineVersion(match []string, inner, next string) string {
	if versionValue.MatchString(inner) {
		updated := versionValue.ReplaceAllStringFunc(inner, func(string) string {
			return "version = " + strconv.Quote(next)
		})
		return match[1] + match[2] + " = {" + updated + "}" + match[4]
	}
	body := strings.TrimRight(inner, " ")
	trail := inner[len(body):]
	return match[1] + match[2] + " = {" + body + ", version = " + strconv.Quote(next) + trail + "}" + match[4]
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		key := filepath.Clean(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
