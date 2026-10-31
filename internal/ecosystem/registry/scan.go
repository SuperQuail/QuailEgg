package registry

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// Scan sources and reasons reported by Scan. The values are stable machine
// identifiers used by the qegg scan output.
const (
	// ScanSourceWorkspace marks a package found by the ecosystem detection.
	ScanSourceWorkspace = "workspace"
	// ScanSourceFile marks a package found by the bounded manifest scan.
	ScanSourceFile = "scan"
	// ScanReasonExcluded marks a candidate skipped by --exclude.
	ScanReasonExcluded = "excluded"
	// ScanReasonInvalidManifest marks a manifest that could not be parsed.
	ScanReasonInvalidManifest = "invalid_manifest"
)

// ScanOptions bounds the extra manifest discovery qegg init performs.
type ScanOptions struct {
	// Depth is the maximum number of directory components below the repository
	// root. Zero disables the bounded scan and keeps only workspace detection.
	Depth int
	// Exclude lists repository-relative glob patterns whose matches are not
	// discovered. A matching ancestor directory excludes its descendants.
	Exclude []string
}

// Scanned is one package candidate found by Scan.
type Scanned struct {
	Package ecosystem.Package
	// Source is ScanSourceWorkspace or ScanSourceFile.
	Source string
	// Reason, when set, explains why qegg must not register the candidate.
	Reason string
	// Detail optionally explains the reason for humans.
	Detail string
}

// defaultScanExcludes are dependency and build directories that never carry a
// package qegg should release, even when a repository forgets to ignore them.
var defaultScanExcludes = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".turbo":       true,
	"coverage":     true,
}

// Scan discovers package candidates. Workspace detection stays authoritative;
// a bounded manifest scan adds packages that are not declared as workspace
// members, never replacing a detected package.
func Scan(ctx context.Context, root string, opts ScanOptions) ([]Scanned, error) {
	if opts.Depth < 0 {
		return nil, fmt.Errorf("scan depth must not be negative: %d", opts.Depth)
	}
	patterns, err := normalizeExcludePatterns(opts.Exclude)
	if err != nil {
		return nil, err
	}
	detected, err := DetectAll(root)
	if err != nil {
		return nil, err
	}
	scanned := make([]Scanned, 0, len(detected))
	seen := map[string]bool{}
	add := func(entry Scanned) {
		key := entry.Package.Resolver + "\x00" + entry.Package.Path
		if seen[key] {
			return
		}
		seen[key] = true
		scanned = append(scanned, entry)
	}
	for _, pkg := range detected {
		entry := Scanned{Package: pkg, Source: ScanSourceWorkspace}
		if scanPathExcluded(pkg.Path, patterns) {
			entry.Reason = ScanReasonExcluded
		}
		add(entry)
	}
	if opts.Depth > 0 {
		files, err := manifestFiles(ctx, root, opts.Depth)
		if err != nil {
			return nil, err
		}
		resolvers := manifestResolvers()
		for _, rel := range files {
			dir := path.Dir(rel)
			for _, id := range resolvers[path.Base(rel)] {
				key := id + "\x00" + dir
				if seen[key] {
					continue
				}
				input := ecosystem.Package{Path: dir, Manifest: rel, Resolver: id}
				if scanPathExcluded(dir, patterns) {
					add(Scanned{Package: input, Source: ScanSourceFile, Reason: ScanReasonExcluded})
					continue
				}
				eco, ok := ByID(id)
				if !ok {
					continue
				}
				scanner, ok := eco.(ecosystem.ManifestScanner)
				if !ok {
					continue
				}
				result, err := scanner.ScanManifest(root, input)
				if err != nil {
					add(Scanned{Package: input, Source: ScanSourceFile, Reason: ScanReasonInvalidManifest, Detail: err.Error()})
					continue
				}
				if result.OK {
					add(Scanned{Package: result.Package, Source: ScanSourceFile})
					continue
				}
				if result.Reason == "" {
					continue
				}
				pkg := result.Package
				if pkg.Path == "" {
					pkg.Path = dir
				}
				if pkg.Manifest == "" {
					pkg.Manifest = rel
				}
				if pkg.Resolver == "" {
					pkg.Resolver = id
				}
				add(Scanned{Package: pkg, Source: ScanSourceFile, Reason: result.Reason, Detail: result.Detail})
			}
		}
	}
	sort.Slice(scanned, func(i, j int) bool {
		left, right := scanned[i].Package, scanned[j].Package
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Resolver != right.Resolver {
			return left.Resolver < right.Resolver
		}
		return left.Name < right.Name
	})
	return scanned, nil
}

// manifestResolvers maps a manifest file name to the ecosystems that parse it.
func manifestResolvers() map[string][]string {
	out := map[string][]string{}
	for _, spec := range Specs() {
		for _, name := range spec.Manifests {
			if name == "" {
				continue
			}
			out[name] = append(out[name], spec.ID)
		}
	}
	return out
}

// manifestFiles lists manifest files tracked or untracked-but-not-ignored by
// git, filtered by depth. Git's listing honours .gitignore, so dependency
// directories and generated trees stay out without qegg reimplementing it.
func manifestFiles(ctx context.Context, root string, depth int) ([]string, error) {
	run := execx.OSRunner{}
	result, err := run.Run(ctx, root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	resolvers := manifestResolvers()
	seen := map[string]bool{}
	var files []string
	for _, entry := range strings.Split(result.Stdout, "\x00") {
		if entry == "" {
			continue
		}
		if strings.Count(entry, "/") > depth {
			continue
		}
		if _, ok := resolvers[path.Base(entry)]; !ok {
			continue
		}
		if seen[entry] {
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry)))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[entry] = true
		files = append(files, entry)
	}
	sort.Strings(files)
	return files, nil
}

// normalizeExcludePatterns trims and validates user supplied globs.
func normalizeExcludePatterns(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))
	for _, raw := range patterns {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}
		if !doublestar.ValidatePattern(pattern) {
			return nil, fmt.Errorf("invalid exclude pattern %q", pattern)
		}
		pattern = strings.TrimPrefix(pattern, "./")
		pattern = strings.TrimSuffix(pattern, "/")
		if pattern == "" {
			continue
		}
		out = append(out, pattern)
	}
	return out, nil
}

// scanPathExcluded reports whether a repository-relative directory is skipped
// by the default blacklist or by an exclude glob. Globs match the directory,
// any ancestor and the root marker.
func scanPathExcluded(rel string, patterns []string) bool {
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == "" {
		clean = "."
	}
	if clean != "." {
		for _, segment := range strings.Split(clean, "/") {
			if defaultScanExcludes[segment] {
				return true
			}
		}
	}
	if matchesAnyPattern(clean, patterns) {
		return true
	}
	for _, ancestor := range ancestorPaths(clean) {
		if matchesAnyPattern(ancestor, patterns) {
			return true
		}
	}
	return false
}

// ancestorPaths lists rel and its parents, excluding the repository root.
func ancestorPaths(rel string) []string {
	var out []string
	current := rel
	for current != "." && current != "/" && current != "" {
		out = append(out, current)
		current = path.Dir(current)
	}
	return out
}

// matchesAnyPattern reports whether candidate matches one of the globs.
func matchesAnyPattern(candidate string, patterns []string) bool {
	for _, pattern := range patterns {
		if ok, err := doublestar.Match(pattern, candidate); err == nil && ok {
			return true
		}
	}
	return false
}
