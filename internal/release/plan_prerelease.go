package release

import (
	"context"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// stableBaseVersion returns the highest stable version the repository knows.
// It combines the newest stable tag with the current manifest version, but only
// when that manifest version is stable: a manifest that already carries a
// prerelease part points at the line being built, not at its base.
func stableBaseVersion(tags []string, prefix, current string) string {
	best := "0.0.0"
	for _, tag := range tags {
		value := strings.TrimPrefix(strings.TrimSpace(tag), prefix)
		if value == "" || semver.IsPrerelease(value) {
			continue
		}
		if cmp, err := semver.Compare(value, best); err == nil && cmp > 0 {
			best = value
		}
	}
	if current != "" && !semver.IsPrerelease(current) {
		if cmp, err := semver.Compare(current, best); err == nil && cmp > 0 {
			best = current
		}
	}
	return best
}

// channelRelease returns the base version and counter of the newest release
// in a prerelease channel.
func channelRelease(tags []string, prefix, channel string) (string, int, bool) {
	base, counter, found := "", 0, false
	for _, tag := range tags {
		value := strings.TrimPrefix(strings.TrimSpace(tag), prefix)
		candidateBase, candidateChannel, candidateCounter, ok := semver.SplitPrerelease(value)
		if !ok || candidateChannel != channel {
			continue
		}
		if !found {
			base, counter, found = candidateBase, candidateCounter, true
			continue
		}
		cmp, err := semver.Compare(candidateBase, base)
		if err != nil {
			continue
		}
		switch {
		case cmp > 0:
			base, counter = candidateBase, candidateCounter
		case cmp == 0 && candidateCounter > counter:
			counter = candidateCounter
		}
	}
	return base, counter, found
}

// sameChangeSet reports whether the pending records are exactly the ones the
// marker already planned, which means no new prerelease is due.
func sameChangeSet(root string, marker *state.Marker, consumed []*changes.Record) bool {
	if marker == nil || len(marker.Changes) != len(consumed) {
		return false
	}
	planned := make(map[string]string, len(marker.Changes))
	for _, change := range marker.Changes {
		planned[change.File] = change.Body
	}
	for _, record := range consumed {
		body, ok := planned[fsx.Rel(root, record.Path)]
		if !ok || body != record.Body {
			return false
		}
	}
	return true
}

// versionPlan fills the version fields of a plan.
func versionPlan(ctx context.Context, d *Deps, plan *Plan, pkgName string, target ecosystem.Package, publishable bool, next, bump string, records []*changes.Record, repo github.Repo) error {
	if err := registry.CheckVersion(target, next); err != nil {
		return err
	}
	planned, err := buildChanges(ctx, d, records, repo)
	if err != nil {
		return err
	}
	plan.Phase = PhaseVersion
	plan.Version = VersionInfo{Current: target.Version, Next: next, Bump: bump}
	plan.Packages = []PackagePlan{{
		Name:     pkgName,
		Path:     target.Path,
		Resolver: target.Resolver,
		Publish:  publishable,
		Current:  target.Version,
		Next:     next,
		Bump:     bump,
		Manifest: target.Manifest,
	}}
	plan.Changes = planned
	plan.Tag = semver.Tag(d.Cfg.Git.TagPrefix, next)
	return nil
}
