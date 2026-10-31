// Package registry is the single entry point for the supported ecosystems:
// it wires the adapters together so callers never depend on them directly.
package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/golang"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/java"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/npm"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/python"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/rust"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// All returns the supported ecosystems.
func All() []ecosystem.Ecosystem {
	return []ecosystem.Ecosystem{npm.New(), python.New(), rust.New(), golang.New(), java.New()}
}

// ByID resolves an ecosystem by identifier.
func ByID(id string) (ecosystem.Ecosystem, bool) {
	for _, eco := range All() {
		if eco.Spec().ID == id {
			return eco, true
		}
	}
	return nil, false
}

// Specs returns the supported ecosystem specifications in a stable order.
func Specs() []ecosystem.Spec {
	all := All()
	out := make([]ecosystem.Spec, 0, len(all))
	for _, eco := range all {
		out = append(out, eco.Spec())
	}
	return out
}

// VersionStyleFor resolves the version spelling of a resolver.
func VersionStyleFor(resolver string) (ecosystem.VersionStyle, bool) {
	eco, ok := ByID(resolver)
	if !ok {
		return nil, false
	}
	style, ok := eco.(ecosystem.VersionStyle)
	return style, ok
}

// PrereleaseVersion renders a prerelease version in the resolver's spelling.
func PrereleaseVersion(resolver, base, channel string, counter int) (string, error) {
	if style, ok := VersionStyleFor(resolver); ok {
		return style.Prerelease(base, channel, counter)
	}
	return semver.Prerelease(base, channel, counter)
}

// SplitPrereleaseVersion parses a version in the resolver's spelling.
func SplitPrereleaseVersion(resolver, version string) (base, channel string, counter int, ok bool) {
	if style, ok := VersionStyleFor(resolver); ok {
		return style.SplitPrerelease(version)
	}
	return semver.SplitPrerelease(version)
}

// TagVersion converts a manifest version into the version a tag carries.
func TagVersion(resolver, version string) string {
	if style, ok := VersionStyleFor(resolver); ok {
		return style.TagVersion(version)
	}
	return version
}

// PublishTargetsFor returns the registry names the resolver publishes to.
func PublishTargetsFor(resolver string) []string {
	spec, ok := SpecFor(resolver)
	if !ok {
		return nil
	}
	return append([]string(nil), spec.PublishTargets...)
}

// PublishTargets returns every publish target name the adapters declare.
func PublishTargets() []string {
	seen := map[string]bool{}
	var out []string
	for _, spec := range Specs() {
		for _, target := range spec.PublishTargets {
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			out = append(out, target)
		}
	}
	sort.Strings(out)
	return out
}

// EcosystemForTarget maps a publish target name to its ecosystem identifier.
func EcosystemForTarget(target string) (string, bool) {
	for _, spec := range Specs() {
		for _, known := range spec.PublishTargets {
			if known == target {
				return spec.ID, true
			}
		}
	}
	return "", false
}

// DefaultVerifyCommands collects the init defaults of every resolver, in a
// stable order and without duplicates.
func DefaultVerifyCommands(root string, resolvers []string) []string {
	names := make([]string, 0, len(resolvers))
	seenResolver := map[string]bool{}
	for _, resolver := range resolvers {
		if resolver == "" || seenResolver[resolver] {
			continue
		}
		seenResolver[resolver] = true
		names = append(names, resolver)
	}
	sort.Strings(names)
	seenCommand := map[string]bool{}
	var out []string
	for _, resolver := range names {
		eco, ok := ByID(resolver)
		if !ok {
			continue
		}
		advisor, ok := eco.(ecosystem.InitAdvisor)
		if !ok {
			continue
		}
		for _, command := range advisor.DefaultVerifyCommands(root) {
			command = strings.TrimSpace(command)
			if command == "" || seenCommand[command] {
				continue
			}
			seenCommand[command] = true
			out = append(out, command)
		}
	}
	return out
}

// EcosystemIDs returns every registered ecosystem identifier in a stable order.
func EcosystemIDs() []string {
	specs := Specs()
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	sort.Strings(out)
	return out
}

// UnknownResolvers returns the names without an adapter, in input order.
func UnknownResolvers(resolvers []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, resolver := range resolvers {
		if resolver == "" || seen[resolver] {
			continue
		}
		seen[resolver] = true
		if _, ok := ByID(resolver); !ok {
			out = append(out, resolver)
		}
	}
	return out
}

// UnknownRegistries returns the names that are not a declared publish target.
func UnknownRegistries(registries []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range registries {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if _, ok := EcosystemForTarget(name); !ok {
			out = append(out, name)
		}
	}
	return out
}

// VersionSyncerFor resolves the per-package version syncer of a resolver.
func VersionSyncerFor(resolver string) (ecosystem.VersionSyncer, bool) {
	eco, ok := ByID(resolver)
	if !ok {
		return nil, false
	}
	syncer, ok := eco.(ecosystem.VersionSyncer)
	return syncer, ok
}

// AssetPlannerFor resolves the asset planner of a resolver.
func AssetPlannerFor(resolver string) (ecosystem.AssetPlanner, bool) {
	eco, ok := ByID(resolver)
	if !ok {
		return nil, false
	}
	planner, ok := eco.(ecosystem.AssetPlanner)
	return planner, ok
}

// DefaultPublish lets an ecosystem veto publication for application packages
// that are not registry artifacts, such as a Tauri app crate.
func DefaultPublish(root string, pkg ecosystem.Package) bool {
	eco, ok := ByID(pkg.Resolver)
	if !ok {
		return pkg.Publish
	}
	advisor, ok := eco.(ecosystem.PublishAdvisor)
	if !ok {
		return pkg.Publish
	}
	return advisor.DefaultPublish(root, pkg)
}

// VersionFileCandidates lists the extra version files an ecosystem suggests
// for a package. Candidates are guesses; qegg init verifies them before use.
func VersionFileCandidates(root string, pkg ecosystem.Package) []ecosystem.VersionFile {
	eco, ok := ByID(pkg.Resolver)
	if !ok {
		return nil
	}
	advisor, ok := eco.(ecosystem.VersionFileAdvisor)
	if !ok {
		return nil
	}
	return advisor.VersionFileCandidates(root, pkg)
}

// SpecFor resolves a specification by identifier.
func SpecFor(id string) (ecosystem.Spec, bool) {
	for _, spec := range Specs() {
		if spec.ID == id {
			return spec, true
		}
	}
	return ecosystem.Spec{}, false
}

// SupportedManifests returns every manifest file name the tool understands.
func SupportedManifests() []string {
	var out []string
	for _, spec := range Specs() {
		out = append(out, spec.Manifests...)
	}
	return out
}

// ManifestListText renders the supported manifests for user-facing messages.
func ManifestListText() string {
	return strings.Join(SupportedManifests(), "、")
}

// DetectAll runs every ecosystem detection and returns packages sorted by path.
func DetectAll(root string) ([]ecosystem.Package, error) {
	var packages []ecosystem.Package
	for _, eco := range All() {
		found, err := eco.Detect(root)
		if err != nil {
			return nil, err
		}
		packages = append(packages, found...)
	}
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Path == packages[j].Path {
			return packages[i].Name < packages[j].Name
		}
		return packages[i].Path < packages[j].Path
	})
	return packages, nil
}

// DetectFor limits detection to the given resolver identifiers.
func DetectFor(root string, ids []string) ([]ecosystem.Package, error) {
	if len(ids) == 0 {
		return DetectAll(root)
	}
	var packages []ecosystem.Package
	for _, id := range ids {
		eco, ok := ByID(id)
		if !ok {
			return nil, fmt.Errorf("unsupported resolver %q", id)
		}
		found, err := eco.Detect(root)
		if err != nil {
			return nil, err
		}
		packages = append(packages, found...)
	}
	return packages, nil
}

// Inspect reads one configured package from its directory. Unlike Detect it
// never scans the repository, so a nested or standalone package works as soon
// as it is registered in .quailegg.toml.
func Inspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	clean, err := cleanPackagePath(root, pkg.Path)
	if err != nil {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_path", pkg.Name, pkg.Path))
	}
	input := pkg
	input.Path = clean
	eco, ok := ByID(pkg.Resolver)
	if !ok {
		return ecosystem.Package{}, fmt.Errorf("unsupported resolver %q for package %q", pkg.Resolver, pkg.Name)
	}
	inspector, ok := eco.(ecosystem.Inspector)
	if !ok {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_resolver", pkg.Resolver))
	}
	inspected, err := inspector.Inspect(root, input)
	if err != nil {
		return ecosystem.Package{}, err
	}
	if inspected.Name != pkg.Name {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_name", pkg.Name, inspected.Manifest, inspected.Name))
	}
	if inspected.Path == "" {
		inspected.Path = clean
	}
	if inspected.Resolver == "" {
		inspected.Resolver = pkg.Resolver
	}
	inspected.Identity = pkg.Identity
	return inspected, nil
}

// InspectAll reads every configured package and sorts them by path.
func InspectAll(root string, pkgs []ecosystem.Package) ([]ecosystem.Package, error) {
	out := make([]ecosystem.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		inspected, err := Inspect(root, pkg)
		if err != nil {
			return nil, err
		}
		out = append(out, inspected)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// cleanPackagePath validates a configured package path and returns it in slash
// form relative to the repository root.
func cleanPackagePath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid package path %q", rel)
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("package path %q escapes the repository", rel)
	}
	abs := filepath.Join(root, filepath.FromSlash(clean))
	within, err := filepath.Rel(root, abs)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("package path %q escapes the repository", rel)
	}
	return clean, nil
}

// IsTagVersioned reports whether the resolver derives its version from git
// release tags instead of a manifest field.
func IsTagVersioned(resolver string) bool {
	eco, ok := ByID(resolver)
	if !ok {
		return false
	}
	_, ok = eco.(ecosystem.TagVersioner)
	return ok
}

// CurrentVersion resolves the current version of pkg. Ecosystems that keep the
// version in the manifest return pkg.Version unchanged.
func CurrentVersion(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, tagPrefix string) (string, error) {
	eco, ok := ByID(pkg.Resolver)
	if !ok {
		return "", fmt.Errorf("unsupported resolver %q for package %q", pkg.Resolver, pkg.Name)
	}
	versioned, ok := eco.(ecosystem.TagVersioner)
	if !ok {
		return pkg.Version, nil
	}
	return versioned.CurrentVersion(ctx, run, root, pkg, tagPrefix)
}

// CheckVersion validates the planned next version against the extra constraints
// of the ecosystem.
func CheckVersion(pkg ecosystem.Package, next string) error {
	eco, ok := ByID(pkg.Resolver)
	if !ok {
		return fmt.Errorf("unsupported resolver %q for package %q", pkg.Resolver, pkg.Name)
	}
	versioned, ok := eco.(ecosystem.TagVersioner)
	if !ok {
		return nil
	}
	return versioned.CheckVersion(pkg, next)
}

// SetVersion writes next into the manifest of pkg.
func SetVersion(root string, pkg ecosystem.Package, next string) error {
	eco, ok := ByID(pkg.Resolver)
	if !ok {
		return fmt.Errorf("unsupported resolver %q for package %q", pkg.Resolver, pkg.Name)
	}
	return eco.SetVersion(root, pkg, next)
}

// PublisherFor resolves the publisher of a resolver identifier.
func PublisherFor(resolver string) (ecosystem.Publisher, bool) {
	eco, ok := ByID(resolver)
	if !ok {
		return nil, false
	}
	return eco.Publisher(), true
}

// Precheck reports whether a version already exists in the registry.
func Precheck(ctx context.Context, client *http.Client, pkg ecosystem.Package, releaseVersion string) (ecosystem.Status, error) {
	publisher, ok := PublisherFor(pkg.Resolver)
	if !ok {
		return ecosystem.Unknown, ecosystem.ErrUnsupported
	}
	return publisher.Precheck(ctx, client, pkg, releaseVersion)
}

// Build prepares dist artefacts when the ecosystem needs them.
func Build(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package) error {
	publisher, ok := PublisherFor(pkg.Resolver)
	if !ok {
		return ecosystem.ErrUnsupported
	}
	return publisher.Build(ctx, run, root, pkg)
}

// Publish uploads the package to its registry.
func Publish(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, releaseVersion string) error {
	publisher, ok := PublisherFor(pkg.Resolver)
	if !ok {
		return ecosystem.ErrUnsupported
	}
	return publisher.Publish(ctx, run, root, pkg, releaseVersion)
}
