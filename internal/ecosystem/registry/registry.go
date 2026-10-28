// Package registry is the single entry point for the supported ecosystems:
// it wires the adapters together so callers never depend on them directly.
package registry

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/golang"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/npm"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/python"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/rust"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// All returns the supported ecosystems.
func All() []ecosystem.Ecosystem {
	return []ecosystem.Ecosystem{npm.New(), python.New(), rust.New(), golang.New()}
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
