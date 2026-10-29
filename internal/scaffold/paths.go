package scaffold

import (
	"path"
	"path/filepath"
	"sort"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
)

// markerPath is the committed release marker the publish phase reads.
const markerPath = ".quailegg/release.json"

// prereleaseLockPath switches the repository into a prerelease channel.
const prereleaseLockPath = ".quailegg/pre-release.lock"

// releasePaths lists the paths whose changes can alter the release plan. The
// generated release workflow runs only when a push touches one of them, so a
// documentation-only commit never starts a release.
func releasePaths(opts Options) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = path.Clean(filepath.ToSlash(value))
		if value == "." || value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}

	changesDir := opts.ChangesDir
	if changesDir == "" {
		changesDir = ".changes"
	}
	add(path.Join(changesDir, "**"))
	add(config.FileName)
	add(markerPath)
	add(prereleaseLockPath)
	add("CHANGELOG.md")

	names := make([]string, 0, len(opts.Packages))
	for name := range opts.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pkg := opts.Packages[name]
		spec, ok := registry.SpecFor(pkg.Resolver)
		if !ok {
			continue
		}
		for _, manifest := range spec.Manifests {
			add(path.Join(pkg.Path, manifest))
		}
	}
	if len(names) == 0 {
		if spec, ok := registry.SpecFor(opts.Ecosystem); ok {
			for _, manifest := range spec.Manifests {
				add(manifest)
			}
		}
	}

	sort.Strings(out)
	return out
}
