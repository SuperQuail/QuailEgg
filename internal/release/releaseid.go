package release

import (
	"errors"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// IdentityFor names the release the plan describes. Everything that has to
// survive a run, a runner or a machine (the recorded snapshot, the publish
// ledger, the repair of a missing GitHub Release) is keyed by this identity.
func IdentityFor(d *Deps, plan *Plan) (state.ReleaseIdentity, error) {
	marker, err := state.LoadMarker(d.Root)
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	if marker == nil {
		return state.ReleaseIdentity{}, errors.New(i18n.T("release.err.no_marker"))
	}
	return identityFrom(d, plan, marker), nil
}

// identityFrom builds the identity from a marker that is already loaded.
func identityFrom(d *Deps, plan *Plan, marker *state.Marker) state.ReleaseIdentity {
	id := state.ReleaseIdentity{SchemaVersion: 1}
	if marker != nil {
		id.Version = marker.Version
		id.Channel = marker.Channel
		id.Mode = marker.Mode
		id.Fingerprint = marker.Fingerprint
		for _, release := range marker.Releases {
			if release.Tag != "" {
				id.Tags = append(id.Tags, release.Tag)
			}
		}
	}
	if plan != nil {
		if id.Version == "" {
			id.Version = plan.Version.Next
		}
		if id.Channel == "" {
			id.Channel = plan.Channel
		}
		if len(id.Tags) == 0 {
			for _, release := range plan.Releases {
				if release.Tag != "" {
					id.Tags = append(id.Tags, release.Tag)
				}
			}
		}
		if id.Repo == "" && plan.Repo.Owner != "" && plan.Repo.Name != "" {
			id.Repo = plan.Repo.Owner + "/" + plan.Repo.Name
		}
	}
	if id.Repo == "" && marker != nil && marker.Repo.Valid() {
		id.Repo = marker.Repo.Owner + "/" + marker.Repo.Name
	}
	if id.Mode == "" && d != nil && d.Cfg != nil {
		id.Mode = d.Cfg.Release.Mode
	}
	sort.Strings(id.Tags)
	return id
}

// describeIdentity renders a release for an error or warning message.
func describeIdentity(id state.ReleaseIdentity) string {
	if len(id.Tags) > 0 {
		return strings.Join(id.Tags, ", ")
	}
	if id.Version != "" {
		return id.Version
	}
	return id.Fingerprint
}
