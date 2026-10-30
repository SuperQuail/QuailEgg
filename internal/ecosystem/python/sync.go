package python

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// dependencySpec matches one PEP 508 dependency pinned with ==.
var dependencySpec = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)(\[[^\]]*\])?\s*==\s*([^;,\s]+)(\s*;.*)?$`)

// SyncWorkspace rewrites exact "==package" pins between workspace packages and
// refreshes uv.lock when the repository has one.
func (driver) SyncWorkspace(ctx context.Context, run execx.Runner, root string, pkgs []ecosystem.Package, version string) ([]string, error) {
	siblings := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		siblings[normalizeName(pkg.Name)] = true
	}
	changed := []string{}
	for _, pkg := range pkgs {
		manifest := filepath.Join(root, filepath.FromSlash(pkg.Path), "pyproject.toml")
		updated, err := rewriteSiblingPins(manifest, siblings, version)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if updated {
			changed = append(changed, path.Join(pkg.Path, "pyproject.toml"))
		}
	}
	lock := filepath.Join(root, "uv.lock")
	if _, err := os.Stat(lock); err == nil {
		if !execx.LookPath("uv") {
			return nil, fmt.Errorf("uv is required to refresh uv.lock")
		}
		if _, err := run.Run(ctx, root, "uv", "lock"); err != nil {
			return nil, err
		}
		changed = append(changed, "uv.lock")
	}
	return changed, nil
}

// rewriteSiblingPins updates exact sibling pins inside [project].dependencies.
func rewriteSiblingPins(manifest string, siblings map[string]bool, version string) (bool, error) {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return false, err
	}
	text := string(data)
	start := strings.Index(text, "[project]")
	if start < 0 {
		return false, nil
	}
	rest := text[start:]
	end := len(rest)
	if idx := strings.Index(rest[1:], "\n["); idx >= 0 {
		end = idx + 1
	}
	section := rest[:end]
	updated := section
	stringRE := regexp.MustCompile(`"([^"]+)"`)
	for _, match := range stringRE.FindAllStringSubmatch(section, -1) {
		spec := dependencySpec.FindStringSubmatch(match[1])
		if spec == nil {
			continue
		}
		if !siblings[normalizeName(spec[1])] || spec[3] == version {
			continue
		}
		replacement := spec[1] + spec[2] + "==" + version + spec[4]
		updated = strings.ReplaceAll(updated, `"`+match[1]+`"`, `"`+replacement+`"`)
	}
	if updated == section {
		return false, nil
	}
	return true, os.WriteFile(manifest, []byte(strings.Replace(text, section, updated, 1)), 0o644)
}
