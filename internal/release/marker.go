package release

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// This file carries the release-marker pipeline: what the committed release
// intent describes, which of its releases are still open, and which of those
// still apply to the checked out tree. shared.go keeps the pure version rules so
// the rules stay testable without git or a marker.

// markerPackages projects the marker's package plans onto the plan.
func markerPackages(packages []state.MarkerPackage) []PackagePlan {
	out := make([]PackagePlan, 0, len(packages))
	for _, pkg := range packages {
		out = append(out, PackagePlan{
			Name: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver,
			Publish: pkg.Publish, Current: pkg.Current, Next: pkg.Next, Bump: pkg.Bump,
			Tag: pkg.Tag,
		})
	}
	return out
}

// markerChanges projects the marker's consumed records onto the plan.
func markerChanges(changes []state.MarkerChange) []ChangePlan {
	out := make([]ChangePlan, 0, len(changes))
	for _, change := range changes {
		out = append(out, ChangePlan{
			File: change.File, Type: change.Type, Scope: change.Scope,
			Bump: change.Bump, Packages: append([]string(nil), change.Packages...), Body: change.Body,
			Commit: change.Commit, CommitURL: change.CommitURL,
			PullNumber: change.PullNumber, PullURL: change.PullURL, Author: change.Author,
		})
	}
	return out
}

// markerReleasePlans expands the marker into the releases it describes.
func markerReleasePlans(d *Deps, marker *state.Marker, targets []target) []ReleasePlan {
	resolver := ""
	if len(targets) > 0 {
		resolver = targets[0].pkg.Resolver
	}
	return markerReleasePlansFor(d.Cfg, marker, targetNames(targets), resolver)
}

// markerReleasePlansFor expands the marker into the releases it describes. A
// marker written before releases were recorded carries one version for the
// whole repository, so its tag and packages are derived from the arguments.
func markerReleasePlansFor(cfg *config.Config, marker *state.Marker, names []string, fallbackResolver string) []ReleasePlan {
	if marker == nil {
		return nil
	}
	if len(marker.Releases) > 0 {
		out := make([]ReleasePlan, 0, len(marker.Releases))
		for _, release := range marker.Releases {
			out = append(out, ReleasePlan{
				Name: release.Name, Version: release.Version, Tag: release.Tag,
				PrevTag: release.PrevTag, Packages: append([]string(nil), release.Packages...),
				Prerelease: release.Prerelease,
			})
		}
		return out
	}
	if marker.Version == "" {
		return nil
	}
	resolver := ""
	packages := make([]string, 0, len(marker.Packages))
	for _, pkg := range marker.Packages {
		if resolver == "" {
			resolver = pkg.Resolver
		}
		packages = append(packages, pkg.Name)
	}
	if resolver == "" {
		resolver = fallbackResolver
	}
	if len(packages) == 0 {
		packages = append(packages, names...)
	}
	return []ReleasePlan{{
		Version:    marker.Version,
		Tag:        tagName(cfg, resolver, marker.Version),
		Packages:   packages,
		Prerelease: marker.Channel,
	}}
}

// openMarkerReleases keeps the marker releases that are still open: no tag yet,
// at least one package, and every package they name still registered. A release
// that names no package is dropped because nothing would be published for it and
// the version comparison in applyingReleases would agree with an empty list.
// Whether such a release still applies to the checked out versions is a separate
// question, answered by applyingReleases: a release can be open and no longer
// applicable, and that is exactly the drift the pending-version message reports.
func openMarkerReleases(ctx context.Context, cfg *config.Config, tags TagChecker, marker *state.Marker, pkgs []PackageVersion) ([]ReleasePlan, error) {
	// A marker written before releases were recorded names no resolver, so the
	// tag has to be spelled like the first package's. An empty resolver renders
	// the default spelling, which differs from the manifest spelling only for an
	// ecosystem that normalises versions in tags (python); there it would look
	// for the wrong tag and hide the pending release.
	resolver := ""
	if len(pkgs) > 0 {
		resolver = pkgs[0].Resolver
	}
	releases := markerReleasePlansFor(cfg, marker, packageVersionNames(pkgs), resolver)
	open := make([]ReleasePlan, 0, len(releases))
	for _, release := range releases {
		exists, err := tags.TagExists(ctx, release.Tag)
		if err != nil {
			return nil, err
		}
		if exists || len(release.Packages) == 0 {
			continue
		}
		registered := true
		for _, name := range release.Packages {
			if _, ok := findPackageVersion(pkgs, name); !ok {
				registered = false
				break
			}
		}
		if registered {
			open = append(open, release)
		}
	}
	return open, nil
}

// applyingReleases keeps the open releases whose version still matches the
// checked out tree. Only an applicable release may enter the publish phase: an
// open release that no longer applies means the plan drifted from the code, and
// publishing it would tag a version the tree never reached.
func applyingReleases(open []ReleasePlan, pkgs []PackageVersion) []ReleasePlan {
	out := make([]ReleasePlan, 0, len(open))
	for _, release := range open {
		// openMarkerReleases already drops a release that names no package, but
		// that is a cross-function invariant. Checking it here keeps the loop
		// below from agreeing with an empty package list, which would let a
		// release that covers nothing reach the publish phase.
		if len(release.Packages) == 0 {
			continue
		}
		applies := true
		for _, name := range release.Packages {
			pkg, ok := findPackageVersion(pkgs, name)
			if !ok || !markerApplies(release.Version, pkg.Version, registry.IsTagVersioned(pkg.Resolver)) {
				applies = false
				break
			}
		}
		if applies {
			out = append(out, release)
		}
	}
	return out
}

// allMarkerTagsExist reports whether every tag the marker describes already
// exists. A failed lookup is returned rather than read as "no tag": the same
// lookup is fatal in openMarkerReleases, and guessing here would plan a
// duplicate prerelease on top of an existing tag.
func allMarkerTagsExist(ctx context.Context, d *Deps, marker *state.Marker, targets []target) (bool, error) {
	releases := markerReleasePlans(d, marker, targets)
	if len(releases) == 0 {
		return false, nil
	}
	for _, release := range releases {
		exists, err := d.Git.TagExists(ctx, release.Tag)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

// packageVersionNames lists the package identities of pkgs in order.
func packageVersionNames(pkgs []PackageVersion) []string {
	names := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		names = append(names, pkg.Name)
	}
	return names
}

// findPackageVersion looks a package up by identity.
func findPackageVersion(pkgs []PackageVersion, name string) (PackageVersion, bool) {
	for _, pkg := range pkgs {
		if pkg.Name == name {
			return pkg, true
		}
	}
	return PackageVersion{}, false
}
