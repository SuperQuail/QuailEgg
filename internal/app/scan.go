package app

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// DefaultScanDepth is how many directory levels below the repository root
// qegg init scans for package manifests.
const DefaultScanDepth = 3

// ScanOptions bounds package discovery.
type ScanOptions struct {
	// Depth is the maximum number of directory components below the root. Zero
	// disables the bounded scan and keeps only workspace detection.
	Depth int
	// Exclude lists repository-relative globs removed from the candidate set.
	Exclude []string
	// Remote names the git remote used to derive the project identity. Empty
	// falls back to the configured remote and then to origin.
	Remote string
}

// ScanProject describes the repository identity shown by qegg scan.
type ScanProject struct {
	DefaultBranch string `json:"default_branch"`
	Remote        string `json:"remote"`
	Repo          string `json:"repo,omitempty"`
	RemoteKnown   bool   `json:"remote_known"`
}

// ScanPackage is one package candidate qegg init may register.
type ScanPackage struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Key        string `json:"key"`
	Path       string `json:"path"`
	Resolver   string `json:"resolver"`
	Version    string `json:"version,omitempty"`
	Inherited  bool   `json:"inherited,omitempty"`
	Publish    bool   `json:"publish"`
	Manifest   string `json:"manifest,omitempty"`
	Source     string `json:"source"`
	Registered bool   `json:"registered"`
	Selectable bool   `json:"selectable"`
	Reason     string `json:"reason,omitempty"`
	Detail     string `json:"detail,omitempty"`
	pkg        ecosystem.Package
}

// ReasonText renders the localized explanation of the candidate reason.
func (p ScanPackage) ReasonText() string {
	return scanReasonText(p.Reason, p.Detail)
}

// DisplayName returns the package name, or its path for an unparsable manifest.
func (p ScanPackage) DisplayName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Path
}

// ScanVersionFile is one extra version file qegg could register.
type ScanVersionFile struct {
	Path       string `json:"path"`
	Format     string `json:"format,omitempty"`
	Key        string `json:"key,omitempty"`
	PackageID  string `json:"package_id"`
	Version    string `json:"version,omitempty"`
	Selectable bool   `json:"selectable"`
	Reason     string `json:"reason,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// ScanResult is the machine readable answer of qegg scan.
type ScanResult struct {
	SchemaVersion   int               `json:"schema_version"`
	Root            string            `json:"root"`
	Depth           int               `json:"depth"`
	Project         ScanProject       `json:"project"`
	Packages        []ScanPackage     `json:"packages"`
	DefaultPackages []string          `json:"default_packages"`
	VersionScope    string            `json:"version_scope"`
	VersionFiles    []ScanVersionFile `json:"version_files"`
	Warnings        []string          `json:"warnings"`
}

// Scan discovers every package candidate qegg init can register. It is
// read-only: it never creates or updates a file.
func Scan(ctx context.Context, root string, opts ScanOptions) (*ScanResult, error) {
	cfg, err := loadConfigIfPresent(root)
	if err != nil {
		return nil, err
	}
	found, err := registry.Scan(ctx, root, registry.ScanOptions{Depth: opts.Depth, Exclude: opts.Exclude})
	if err != nil {
		return nil, err
	}
	for index := range found {
		found[index].Package.Publish = registry.DefaultPublish(root, found[index].Package)
	}
	result := &ScanResult{SchemaVersion: 1, Root: root, Depth: opts.Depth}
	result.Project, result.Warnings = scanProject(ctx, root, opts.Remote, cfg)
	result.Packages = buildScanPackages(found, cfg)
	for _, pkg := range result.Packages {
		if pkg.Selectable && !pkg.Registered {
			result.DefaultPackages = append(result.DefaultPackages, pkg.ID)
		}
	}
	result.VersionScope = scanVersionScope(cfg, result.Packages)
	result.VersionFiles = scanVersionFiles(root, cfg, result.Packages, result.VersionScope)
	result.Warnings = append(result.Warnings, scanWarnings(cfg, result.Packages)...)
	if result.Packages == nil {
		result.Packages = []ScanPackage{}
	}
	if result.DefaultPackages == nil {
		result.DefaultPackages = []string{}
	}
	if result.VersionFiles == nil {
		result.VersionFiles = []ScanVersionFile{}
	}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	return result, nil
}

// SelectPackages resolves the --packages selectors into canonical candidate
// IDs. A nil selector list means "every default package" and returns nil; a
// non-nil empty list means "none".
func SelectPackages(result *ScanResult, selectors []string) ([]string, error) {
	if selectors == nil {
		return nil, nil
	}
	byID := map[string]int{}
	byKey := map[string]int{}
	byName := map[string][]int{}
	for index := range result.Packages {
		pkg := result.Packages[index]
		byID[pkg.ID] = index
		if pkg.Key != "" {
			byKey[pkg.Key] = index
		}
		if pkg.Name != "" {
			byName[pkg.Name] = append(byName[pkg.Name], index)
		}
	}
	out := make([]string, 0, len(selectors))
	seen := map[string]bool{}
	for _, raw := range selectors {
		selector := strings.TrimSpace(raw)
		if selector == "" {
			continue
		}
		index, err := findScanPackage(byID, byKey, byName, selector)
		if err != nil {
			return nil, err
		}
		pkg := result.Packages[index]
		if !pkg.Selectable && !pkg.Registered {
			return nil, errors.New(i18n.Tf("init.err.package_unselectable", selector, pkg.ReasonText()))
		}
		if seen[pkg.ID] {
			continue
		}
		seen[pkg.ID] = true
		out = append(out, pkg.ID)
	}
	return out, nil
}

// loadConfigIfPresent reads .quailegg.toml, returning nil when it does not
// exist. The content is returned unchanged so callers can merge into it.
func loadConfigIfPresent(root string) (*config.Config, error) {
	configPath := filepath.Join(root, config.FileName)
	content, err := fsx.ReadOrEmpty(configPath)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, nil
	}
	return config.LoadFile(configPath)
}

// scanProject resolves the branch and repository identity, turning a missing
// remote into a warning because qegg scan works before qegg init.
func scanProject(ctx context.Context, root, remote string, cfg *config.Config) (ScanProject, []string) {
	project := ScanProject{DefaultBranch: detectDefaultBranch(ctx, root), Remote: remote}
	if project.Remote == "" && cfg != nil {
		project.Remote = cfg.Git.Remote
	}
	if project.Remote == "" {
		project.Remote = "origin"
	}
	repo, warning, err := detectRemoteRepo(ctx, root, project.Remote, cfg)
	if err != nil {
		return project, []string{err.Error()}
	}
	project.Repo = repo
	project.RemoteKnown = true
	if warning == "" {
		return project, nil
	}
	return project, []string{warning}
}

// buildScanPackages applies the registration policy to the registry result.
func buildScanPackages(found []registry.Scanned, cfg *config.Config) []ScanPackage {
	conflicts := map[string]int{}
	for _, entry := range found {
		if entry.Package.Name != "" {
			conflicts[entry.Package.Resolver+"\x00"+entry.Package.Name]++
		}
	}
	knownKeys := map[string]bool{}
	registeredNames := map[string]string{}
	if cfg != nil {
		for _, key := range cfg.PackageNames() {
			knownKeys[key] = true
			registeredNames[cfg.ManifestName(key)] = key
		}
	}
	out := make([]ScanPackage, 0, len(found))
	for _, entry := range found {
		pkg := entry.Package
		item := ScanPackage{
			ID:         scanPackageID(pkg),
			Name:       pkg.Name,
			Key:        suggestedPackageKey(pkg.Resolver, pkg.Name),
			Path:       pkg.Path,
			Resolver:   pkg.Resolver,
			Version:    pkg.Version,
			Inherited:  pkg.Inherited,
			Publish:    pkg.Publish,
			Manifest:   pkg.Manifest,
			Source:     entry.Source,
			Selectable: entry.Reason == "",
			Reason:     entry.Reason,
			Detail:     entry.Detail,
			pkg:        pkg,
		}
		if cfg != nil && pkg.Name != "" {
			if key, ok := registeredPackageKey(cfg, pkg); ok {
				item.Registered = true
				item.Key = key
			}
		}
		if item.Selectable && !item.Registered {
			switch {
			case pkg.Name == "":
				item.Selectable = false
				item.Reason = "invalid_manifest"
			case pkg.Version == "" && !registry.IsTagVersioned(pkg.Resolver):
				item.Selectable = false
				item.Reason = "no_version"
				item.Detail = i18n.Tf("scan.detail.no_version", scanDisplayName(pkg))
			case conflicts[pkg.Resolver+"\x00"+pkg.Name] > 1:
				item.Selectable = false
				item.Reason = "duplicate_name"
				item.Detail = i18n.Tf("scan.detail.duplicate_name", pkg.Name)
			case registeredNames[pkg.Name] != "":
				item.Selectable = false
				item.Reason = "duplicate_name"
				item.Detail = i18n.Tf("scan.detail.name_registered", pkg.Name, registeredNames[pkg.Name])
			case knownKeys[item.Key]:
				item.Selectable = false
				item.Reason = "key_conflict"
				item.Detail = i18n.Tf("scan.detail.key_conflict", item.Key)
			}
		}
		out = append(out, item)
	}
	return out
}

// suggestedPackageKey renders the resolver-qualified identity qegg init would
// write for a manifest name.
func suggestedPackageKey(resolver, name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "@")
	if name == "" {
		return ""
	}
	return resolver + "@" + name
}

// registeredPackageKey finds the configuration key for a detected package.
func registeredPackageKey(cfg *config.Config, pkg ecosystem.Package) (string, bool) {
	for _, key := range cfg.PackageNames() {
		configured := cfg.Packages[key]
		if config.ManifestNameOf(key, configured) != pkg.Name {
			continue
		}
		if configured.Resolver != pkg.Resolver {
			continue
		}
		if !sameRepoPath(configured.Path, pkg.Path) {
			continue
		}
		return key, true
	}
	return "", false
}

// sameRepoPath compares two repository-relative paths.
func sameRepoPath(left, right string) bool {
	clean := func(value string) string {
		return path.Clean(filepath.ToSlash(strings.TrimSpace(value)))
	}
	return clean(left) == clean(right)
}

// scanVersionScope predicts the scope plain qegg init would use.
func scanVersionScope(cfg *config.Config, packages []ScanPackage) string {
	if cfg != nil && cfg.Release.VersionScope != "" {
		return cfg.Release.VersionScope
	}
	detected := make([]ecosystem.Package, 0, len(packages))
	for _, pkg := range packages {
		if pkg.Selectable && !pkg.Registered {
			detected = append(detected, pkg.pkg)
		}
	}
	if len(detected) > 1 && uniformVersions(detected) {
		return config.VersionScopeShared
	}
	return config.VersionScopePackage
}

// scanVersionFiles lists the extra version files plain qegg init would write
// and whether the predicted scope allows registering them.
func scanVersionFiles(root string, cfg *config.Config, packages []ScanPackage, scope string) []ScanVersionFile {
	known := map[string]bool{}
	if cfg != nil {
		for _, file := range cfg.Version.File {
			if rel, err := file.RepoPath(); err == nil {
				known[rel] = true
			}
		}
	}
	total := 0
	if cfg != nil {
		total += len(cfg.Packages)
	}
	for _, pkg := range packages {
		if pkg.Selectable && !pkg.Registered {
			total++
		}
	}
	var out []ScanVersionFile
	seen := map[string]bool{}
	for _, pkg := range packages {
		if !pkg.Selectable || pkg.Registered {
			continue
		}
		for _, candidate := range registry.VersionFileCandidates(root, pkg.pkg) {
			rel := path.Clean(path.Join(pkg.Path, filepath.ToSlash(candidate.Path)))
			if known[rel] || seen[rel] {
				continue
			}
			seen[rel] = true
			file := config.VersionFile{Path: rel, Format: candidate.Format, Key: candidate.Key}
			entry := ScanVersionFile{
				Path:       rel,
				Format:     candidate.Format,
				Key:        candidate.Key,
				PackageID:  pkg.ID,
				Selectable: true,
			}
			value, err := file.Read(root)
			if err != nil {
				entry.Selectable = false
				entry.Reason = "invalid_version_file"
				entry.Detail = err.Error()
			} else {
				entry.Version = value
				if _, err := semver.Parse(value); err != nil {
					entry.Selectable = false
					entry.Reason = "invalid_version_file"
					entry.Detail = err.Error()
				}
			}
			if entry.Selectable && total > 1 && scope != config.VersionScopeShared {
				entry.Selectable = false
				entry.Reason = "needs_shared_scope"
			}
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// scanWarnings reports repository state a plain init would act on.
func scanWarnings(cfg *config.Config, packages []ScanPackage) []string {
	var warnings []string
	if cfg == nil {
		warnings = append(warnings, i18n.T("scan.warn.no_config"))
	}
	if len(packages) == 0 {
		warnings = append(warnings, i18n.T("scan.warn.none"))
	}
	if cfg != nil {
		detected := map[string]bool{}
		for _, pkg := range packages {
			if pkg.Name != "" {
				detected[pkg.Name] = true
			}
		}
		var missing []string
		for _, key := range cfg.PackageNames() {
			if !detected[cfg.ManifestName(key)] {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			warnings = append(warnings, i18n.Tf("scan.warn.configured_missing", strings.Join(missing, ", ")))
		}
	}
	return warnings
}

// scanPackageID is the stable identifier used by --packages.
func scanPackageID(pkg ecosystem.Package) string {
	return pkg.Resolver + ":" + pkg.Path
}

// scanDisplayName returns a printable name for a candidate.
func scanDisplayName(pkg ecosystem.Package) string {
	if pkg.Name != "" {
		return pkg.Name
	}
	return pkg.Path
}

// scanReasonText prefers the localized detail and falls back to the reason key.
func scanReasonText(reason, detail string) string {
	if detail != "" {
		return detail
	}
	if reason == "" {
		return ""
	}
	key := "scan.reason." + reason
	text := i18n.T(key)
	if text == key {
		return reason
	}
	return text
}

// findScanPackage resolves one selector to a candidate index. Selectors may be
// the stable id, the identity key, or a unique manifest name.
func findScanPackage(byID, byKey map[string]int, byName map[string][]int, selector string) (int, error) {
	if index, ok := byID[selector]; ok {
		return index, nil
	}
	if index, ok := byKey[selector]; ok {
		return index, nil
	}
	matches := byName[selector]
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return 0, errors.New(i18n.Tf("init.err.package_unknown", selector))
	default:
		return 0, errors.New(i18n.Tf("init.err.package_ambiguous", selector))
	}
}
