package release

import (
	"context"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// A shared repository has to sit on one version. Without a pending release the
// first package is the reference, so only the packages behind it are reported.
func TestCheckSharedVersionsRequiresOneVersion(t *testing.T) {
	pkgs := []PackageVersion{
		{Name: "npm@frontend", Version: "0.1.0", Resolver: "npm"},
		{Name: "go@demo", Version: "0.0.0", Resolver: "go"},
	}
	violations := CheckSharedVersions(pkgs, "")
	if len(violations) != 1 {
		t.Fatalf("violations = %+v, want exactly one", violations)
	}
	got := violations[0]
	if got.Code != SharedVersionMismatch {
		t.Errorf("Code = %q, want %q", got.Code, SharedVersionMismatch)
	}
	if got.Package != "go@demo" {
		t.Errorf("Package = %q, want %q", got.Package, "go@demo")
	}
	if got.Version != "0.0.0" {
		t.Errorf("Version = %q, want %q", got.Version, "0.0.0")
	}
	if got.Other != "npm@frontend" {
		t.Errorf("Other = %q, want %q", got.Other, "npm@frontend")
	}
	if got.OtherVersion != "0.1.0" {
		t.Errorf("OtherVersion = %q, want %q", got.OtherVersion, "0.1.0")
	}
}

// The version phase writes the pending version into the manifest packages while
// a tag-versioned package keeps its previous tag, so the pending version is the
// reference and the two current versions may differ.
func TestCheckSharedVersionsAcceptsPendingRelease(t *testing.T) {
	pkgs := []PackageVersion{
		{Name: "npm@frontend", Version: "0.1.0-alpha.1", Resolver: "npm"},
		{Name: "go@demo", Version: "0.0.0", Resolver: "go"},
	}
	if violations := CheckSharedVersions(pkgs, "0.1.0-alpha.1"); len(violations) != 0 {
		t.Fatalf("violations = %+v, want none for a pending release", violations)
	}
}

// A tag-versioned package already released beyond the planned version cannot be
// part of that release: the tag it would create already exists.
func TestCheckSharedVersionsRejectsTagAheadOfPending(t *testing.T) {
	pkgs := []PackageVersion{
		{Name: "npm@frontend", Version: "0.1.0-alpha.1", Resolver: "npm"},
		{Name: "go@demo", Version: "0.2.0", Resolver: "go"},
	}
	violations := CheckSharedVersions(pkgs, "0.1.0-alpha.1")
	if len(violations) != 1 {
		t.Fatalf("violations = %+v, want exactly one", violations)
	}
	got := violations[0]
	if got.Code != SharedVersionPending {
		t.Errorf("Code = %q, want %q", got.Code, SharedVersionPending)
	}
	if got.Package != "go@demo" {
		t.Errorf("Package = %q, want %q", got.Package, "go@demo")
	}
	if got.Version != "0.2.0" {
		t.Errorf("Version = %q, want %q", got.Version, "0.2.0")
	}
	if got.OtherVersion != "0.1.0-alpha.1" {
		t.Errorf("OtherVersion = %q, want %q", got.OtherVersion, "0.1.0-alpha.1")
	}
}

// A manifest package that never reached the pending version would publish the
// old version under the new tag, so it is a violation too. The tag-versioned
// package at the same 0.0.0 is not: it keeps its previous tag until the pending
// one is created, so only the manifest package may be reported.
func TestCheckSharedVersionsRejectsManifestBehindPending(t *testing.T) {
	pkgs := []PackageVersion{
		{Name: "npm@frontend", Version: "0.0.0", Resolver: "npm"},
		{Name: "go@demo", Version: "0.0.0", Resolver: "go"},
	}
	violations := CheckSharedVersions(pkgs, "0.1.0")
	if len(violations) != 1 {
		t.Fatalf("violations = %+v, want exactly one for the manifest package", violations)
	}
	got := violations[0]
	if got.Package != "npm@frontend" {
		// The tag-versioned package keeps its previous tag until the pending one
		// exists, so the same 0.0.0 must not be blamed on it.
		t.Fatalf("Package = %q, want %q", got.Package, "npm@frontend")
	}
	if got.Code != SharedVersionPending {
		t.Errorf("Code = %q, want %q", got.Code, SharedVersionPending)
	}
	if got.Version != "0.0.0" {
		t.Errorf("Version = %q, want %q", got.Version, "0.0.0")
	}
	if got.OtherVersion != "0.1.0" {
		t.Errorf("OtherVersion = %q, want %q", got.OtherVersion, "0.1.0")
	}
}

// One tag carries the version of every package, so the ecosystems have to spell
// that version the same way. PackageTag is the offending package's spelling,
// ReferenceTag the reference package's spelling, which is what the message
// renders.
func TestCheckSharedVersionsRejectsTagSpelling(t *testing.T) {
	pkgs := []PackageVersion{
		{Name: "python@core", Version: "2.0.2-a14", Resolver: "python"},
		{Name: "npm@frontend", Version: "2.0.2-a14", Resolver: "npm"},
	}
	violations := CheckSharedVersions(pkgs, "")
	if len(violations) != 1 {
		t.Fatalf("violations = %+v, want exactly one", violations)
	}
	got := violations[0]
	if got.Code != SharedTagSpelling {
		t.Errorf("Code = %q, want %q", got.Code, SharedTagSpelling)
	}
	if got.Package != "npm@frontend" {
		t.Errorf("Package = %q, want %q", got.Package, "npm@frontend")
	}
	if got.Other != "python@core" {
		t.Errorf("Other = %q, want %q", got.Other, "python@core")
	}
	if got.PackageTag != "2.0.2-a14" {
		t.Errorf("PackageTag = %q, want the npm spelling %q", got.PackageTag, "2.0.2-a14")
	}
	if got.ReferenceTag != "2.0.2a14" {
		t.Errorf("ReferenceTag = %q, want the python spelling %q", got.ReferenceTag, "2.0.2a14")
	}
}

// A version problem explains any spelling difference, so the check reports the
// version alone instead of burying it under a second rule.
func TestCheckSharedVersionsSkipsSpellingWhenVersionsDiffer(t *testing.T) {
	pkgs := []PackageVersion{
		{Name: "python@core", Version: "2.0.2-a14", Resolver: "python"},
		{Name: "npm@frontend", Version: "1.0.0", Resolver: "npm"},
	}
	violations := CheckSharedVersions(pkgs, "")
	if len(violations) == 0 {
		t.Fatal("violations = none, want the version mismatch")
	}
	for _, violation := range violations {
		if violation.Code == SharedTagSpelling {
			t.Fatalf("violations = %+v, want no tag-spelling violation while the versions differ", violations)
		}
	}
}

// fakeTags answers TagExists from a fixed set of tags.
type fakeTags map[string]bool

func (f fakeTags) TagExists(_ context.Context, name string) (bool, error) {
	return f[name], nil
}

// A pending release that leaves a package out still names the version it is
// preparing: the version is what the user has to compare against, and the
// uncovered packages explain why the release cannot be published as planned.
func TestPendingSharedVersionReportsUncoveredPackages(t *testing.T) {
	marker := &state.Marker{
		Version: "0.1.0",
		Releases: []state.MarkerRelease{{
			Version: "0.1.0", Tag: "v0.1.0", Packages: []string{"npm@frontend"},
		}},
	}
	pkgs := []PackageVersion{
		{Name: "go@demo", Version: "0.0.0", Resolver: "go"},
		{Name: "npm@frontend", Version: "0.1.0", Resolver: "npm"},
	}
	pending, err := PendingSharedVersion(context.Background(), config.Default(), fakeTags{}, marker, pkgs)
	if err != nil {
		t.Fatalf("PendingSharedVersion: %v", err)
	}
	if pending.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", pending.Version, "0.1.0")
	}
	if len(pending.Uncovered) != 1 || pending.Uncovered[0] != "go@demo" {
		t.Errorf("Uncovered = %v, want [go@demo]", pending.Uncovered)
	}
}

// An open release and an applicable release are different questions: the
// candidate list keeps a release whose version no longer matches the tree, so
// the plan can report the drift instead of silently starting over. Only the
// applicable list may reach the publish phase.
func TestOpenMarkerReleasesKeepsInapplicableRelease(t *testing.T) {
	marker := &state.Marker{
		Version: "0.1.0",
		Releases: []state.MarkerRelease{{
			Version: "0.1.0", Tag: "v0.1.0", Packages: []string{"go@demo", "npm@frontend"},
		}},
	}
	pkgs := []PackageVersion{
		{Name: "go@demo", Version: "0.0.0", Resolver: "go"},
		{Name: "npm@frontend", Version: "0.0.0", Resolver: "npm"},
	}
	open, err := openMarkerReleases(context.Background(), config.Default(), fakeTags{}, marker, pkgs)
	if err != nil {
		t.Fatalf("openMarkerReleases: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("open = %+v, want the release to stay a candidate", open)
	}
	if got := applyingReleases(open, pkgs); len(got) != 0 {
		t.Fatalf("applying = %+v, want none while the manifest never reached 0.1.0", got)
	}
	// Once the manifest carries the pending version the release applies again.
	pkgs[1].Version = "0.1.0"
	if got := applyingReleases(open, pkgs); len(got) != 1 {
		t.Fatalf("applying = %+v, want the release to apply", got)
	}
}
