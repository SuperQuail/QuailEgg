package release

import (
	"context"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// stableBaseVersions returns the highest stable version the unit knows. It
// combines the newest stable tag with the current version when that version is
// stable: a manifest that already carries a prerelease part points at the line
// being built, not at its base.
func stableBaseVersions(versions []string, resolver, current string) string {
	best := "0.0.0"
	for _, value := range versions {
		if value == "" {
			continue
		}
		if _, _, _, prerelease := registry.SplitPrereleaseVersion(resolver, value); prerelease {
			continue
		}
		if cmp, err := semver.Compare(value, best); err == nil && cmp > 0 {
			best = value
		}
	}
	if current != "" {
		if _, _, _, prerelease := registry.SplitPrereleaseVersion(resolver, current); !prerelease {
			if cmp, err := semver.Compare(current, best); err == nil && cmp > 0 {
				best = current
			}
		}
	}
	return best
}

// channelReleaseVersions returns the base version and counter of the newest
// release in a prerelease channel.
func channelReleaseVersions(versions []string, resolver, channel string) (string, int, bool) {
	base, counter, found := "", 0, false
	for _, value := range versions {
		candidateBase, candidateChannel, candidateCounter, ok := registry.SplitPrereleaseVersion(resolver, value)
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

// unitTagVersions extracts the version part of every tag that belongs to the
// unit. Shared releases use the tag prefix; package-scoped releases parse the
// configured (or default) tag format.
func unitTagVersions(tags []string, cfg *config.Config, scope string, unit releaseUnit) []string {
	versions := []string{}
	if scope != config.VersionScopeShared && len(cfg.Packages) > 1 {
		format := strings.TrimSpace(cfg.Git.TagFormat)
		if format == "" {
			format = defaultTagFormat
		}
		rendered := renderTagFormat(format, unit.name, "{{version}}")
		parts := strings.SplitN(rendered, "{{version}}", 2)
		prefix, suffix := parts[0], parts[1]
		for _, tag := range tags {
			if !strings.HasPrefix(tag, prefix) || !strings.HasSuffix(tag, suffix) {
				continue
			}
			if len(tag) < len(prefix)+len(suffix) {
				continue
			}
			value := tag[len(prefix) : len(tag)-len(suffix)]
			if value != "" {
				versions = append(versions, value)
			}
		}
		return versions
	}
	prefix := cfg.Git.TagPrefix
	for _, tag := range tags {
		if !strings.HasPrefix(tag, prefix) {
			continue
		}
		value := strings.TrimPrefix(tag, prefix)
		if value != "" {
			versions = append(versions, value)
		}
	}
	return versions
}

// stableBaseTag returns the tag of the last stable release of a unit. The
// graduation plan links its notes to that tag: the records aggregate everything
// since the last stable release even though the manifest carries a prerelease
// version. A missing stable tag returns "" so the caller can fall back to the
// prerelease tag.
func stableBaseTag(ctx context.Context, d *Deps, scope string, unit releaseUnit, tags []string) string {
	if len(unit.targets) == 0 {
		return ""
	}
	resolver := unit.targets[0].pkg.Resolver
	versions := unitTagVersions(tags, d.Cfg, scope, unit)
	base := stableBaseVersions(versions, resolver, "")
	tag, err := packageTag(d.Cfg, scope, unit.targets[0].pkg, base)
	if err != nil {
		return ""
	}
	exists, err := d.Git.TagExists(ctx, tag)
	if err != nil || !exists {
		return ""
	}
	return tag
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
