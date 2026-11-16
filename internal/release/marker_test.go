package release

import (
	"context"
	"errors"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// errTagLookup is the fixed failure the tag fakes below report.
var errTagLookup = errors.New("tag lookup failed")

// failingTags is a TagChecker whose every lookup fails.
type failingTags struct{ err error }

func (f failingTags) TagExists(context.Context, string) (bool, error) { return false, f.err }

// failingGit is a GitOps whose every tag lookup fails. GitOps is embedded to
// satisfy the interface without stubbing every method: the marker pipeline only
// calls TagExists.
type failingGit struct {
	GitOps
	err error
}

func (f failingGit) TagExists(context.Context, string) (bool, error) { return false, f.err }

// pendingMarker is the marker the failure tests read: one release that has no
// tag yet, so the lookup is the only thing that can go wrong.
func pendingMarker() *state.Marker {
	return &state.Marker{
		Version: "0.1.0",
		Releases: []state.MarkerRelease{{
			Version: "0.1.0", Tag: "v0.1.0", Packages: []string{"npm@frontend"},
		}},
	}
}

// A failed tag lookup must not read as "no tag": the release would look open and
// the publish phase would run against a tag that may already exist.
func TestOpenMarkerReleasesPropagatesTagLookupFailure(t *testing.T) {
	pkgs := []PackageVersion{{Name: "npm@frontend", Version: "0.1.0", Resolver: "npm"}}
	open, err := openMarkerReleases(context.Background(), config.Default(), failingTags{err: errTagLookup}, pendingMarker(), pkgs)
	if !errors.Is(err, errTagLookup) {
		t.Fatalf("openMarkerReleases error = %v, want %v", err, errTagLookup)
	}
	if open != nil {
		t.Fatalf("open = %+v, want no release alongside the error", open)
	}
}

// The same lookup is fatal in allMarkerTagsExist: reading a failure as "the tag
// is missing" would plan a duplicate prerelease on top of an existing tag.
func TestAllMarkerTagsExistPropagatesTagLookupFailure(t *testing.T) {
	d := &Deps{Cfg: config.Default(), Git: failingGit{err: errTagLookup}}
	targets := []target{{
		name: "npm@frontend",
		pkg:  ecosystem.Package{Name: "frontend", Path: "frontend", Resolver: config.ResolverNPM, Version: "0.1.0"},
	}}
	exists, err := allMarkerTagsExist(context.Background(), d, pendingMarker(), targets)
	if !errors.Is(err, errTagLookup) {
		t.Fatalf("allMarkerTagsExist error = %v, want %v", err, errTagLookup)
	}
	if exists {
		t.Fatal("allMarkerTagsExist = true alongside the error, want false")
	}
}

// A release that names no package cannot be compared against the tree, so it must
// not slip through as "applies": the publish phase would tag a version no package
// ever reached.
func TestApplyingReleasesSkipsReleaseWithoutPackages(t *testing.T) {
	open := []ReleasePlan{{Version: "0.1.0", Tag: "v0.1.0"}}
	pkgs := []PackageVersion{{Name: "npm@frontend", Version: "0.1.0", Resolver: "npm"}}
	if got := applyingReleases(open, pkgs); len(got) != 0 {
		t.Fatalf("applying = %+v, want none for a release that names no package", got)
	}
}
