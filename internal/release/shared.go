package release

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// PackageVersion is the minimum a shared-version check needs to know about one
// registered package.
type PackageVersion struct {
	Name     string
	Version  string
	Resolver string
}

// Codes reported by CheckSharedVersions.
const (
	SharedVersionMismatch = "version-mismatch"
	SharedVersionPending  = "version-pending"
	SharedTagSpelling     = "tag-spelling"
)

// SharedViolation is one broken shared-version rule.
type SharedViolation struct {
	Code    string
	Package string
	Version string
	// Other is the reference package the violation compares against. A version
	// violation leaves it empty when the reference version comes from the pending
	// release instead of from a checked out package, because no package carries
	// that version yet. A tag-spelling violation always names the reference
	// package: it compares two spellings of one planned version, and the package
	// that renders it is known whether the version is pending or checked out.
	Other string
	// OtherVersion is the version the violation compares against: the first
	// package's version, or the pending release version when one is in flight.
	OtherVersion string
	// PackageTag is the tag spelling the offending package would produce.
	PackageTag string
	// ReferenceTag is the tag spelling of the package the violation refers to.
	ReferenceTag string
}

// TagChecker is the git surface a pending-release check needs.
type TagChecker interface {
	TagExists(ctx context.Context, name string) (bool, error)
}

// PendingRelease is the release the marker is preparing.
type PendingRelease struct {
	Version string
	// Uncovered lists the packages the pending release does not include. They
	// never reach its version, so the release has to be planned again.
	Uncovered []string
}

// CheckSharedVersions reports the packages that do not agree on one version.
// With a pending release the pending version is the reference: the version
// phase already wrote it into the manifest-versioned packages while a
// tag-versioned package keeps its previous tag until the new tag exists, so the
// current versions of a shared unit legitimately differ in between. Without a
// pending release the packages have to be in step already.
func CheckSharedVersions(pkgs []PackageVersion, pending string) []SharedViolation {
	if len(pkgs) < 2 {
		return nil
	}
	reference := pending
	if reference == "" {
		reference = pkgs[0].Version
	}
	// Without a pending release the first package defines the reference, so
	// only the others can disagree with it.
	first := 0
	if pending == "" {
		first = 1
	}
	var violations []SharedViolation
	for _, pkg := range pkgs[first:] {
		// The rule lives in markerApplies so the plan and the check cannot drift
		// apart. Without a pending release the tag-versioned exemption is off,
		// which turns markerApplies into an exact equality: the relaxation only
		// exists because a tag-versioned package keeps its previous tag until the
		// pending one is created.
		agrees := markerApplies(reference, pkg.Version, pending != "" && registry.IsTagVersioned(pkg.Resolver))
		if agrees {
			continue
		}
		violation := SharedViolation{
			Code: SharedVersionMismatch, Package: pkg.Name, Version: pkg.Version, OtherVersion: reference,
		}
		if pending != "" {
			violation.Code = SharedVersionPending
		} else {
			violation.Other = pkgs[0].Name
		}
		violations = append(violations, violation)
	}
	if len(violations) > 0 {
		// A version problem explains any tag spelling difference, so report it
		// alone instead of burying it under a second rule.
		return violations
	}
	// One tag carries the version of every package in the unit, so the
	// ecosystems have to spell that version the same way in a tag.
	want := registry.TagVersion(pkgs[0].Resolver, reference)
	for _, pkg := range pkgs[1:] {
		if have := registry.TagVersion(pkg.Resolver, reference); have != want {
			violations = append(violations, SharedViolation{
				Code: SharedTagSpelling, Package: pkg.Name, Other: pkgs[0].Name,
				OtherVersion: reference, PackageTag: have, ReferenceTag: want,
			})
		}
	}
	return violations
}

// PendingSharedVersion returns the release the marker is preparing. The version
// is empty when the marker does not describe exactly one open release; the
// packages it does not cover are reported separately because a release that
// misses a package cannot be published even though its version is known.
func PendingSharedVersion(ctx context.Context, cfg *config.Config, tags TagChecker, marker *state.Marker, pkgs []PackageVersion) (PendingRelease, error) {
	if marker == nil || len(pkgs) == 0 {
		return PendingRelease{}, nil
	}
	open, err := openMarkerReleases(ctx, cfg, tags, marker, pkgs)
	if err != nil {
		return PendingRelease{}, err
	}
	version, uncovered := sharedPendingVersion(open, packageVersionNames(pkgs))
	return PendingRelease{Version: version, Uncovered: uncovered}, nil
}

// sharedPendingVersion returns the version of the one open release and the
// packages it does not cover. More than one open release leaves the plan
// without a reference: two versions cannot be pending at the same time.
func sharedPendingVersion(open []ReleasePlan, names []string) (string, []string) {
	if len(open) != 1 {
		return "", nil
	}
	covered := make(map[string]bool, len(open[0].Packages))
	for _, name := range open[0].Packages {
		covered[name] = true
	}
	var uncovered []string
	for _, name := range names {
		if !covered[name] {
			uncovered = append(uncovered, name)
		}
	}
	return open[0].Version, uncovered
}

// sharedPackages projects the targets onto the shared-version check input.
func sharedPackages(targets []target) []PackageVersion {
	pkgs := make([]PackageVersion, 0, len(targets))
	for _, t := range targets {
		pkgs = append(pkgs, PackageVersion{Name: t.name, Version: t.pkg.Version, Resolver: t.pkg.Resolver})
	}
	return pkgs
}
