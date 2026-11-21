package release

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// ResumePlan returns the plan a resume has to execute. The planner answers for a
// release that still has work to plan; when it finds nothing (the tag already
// exists, the change records were consumed) the marker still names the release
// that was interrupted, and the publish phase decides per package and per step
// what is left. It returns (nil, nil) when the repository records no release.
func ResumePlan(ctx context.Context, d *Deps) (*Plan, error) {
	plan, err := Build(ctx, d)
	if err != nil {
		return nil, err
	}
	marker, err := state.LoadMarker(d.Root)
	if err != nil {
		return nil, err
	}
	if marker == nil {
		if plan.Phase == PhaseNone {
			return nil, nil
		}
		return plan, nil
	}
	open, err := markerReleaseOpen(ctx, d, marker)
	if err != nil {
		return nil, err
	}
	if open {
		// The marker names a release that is not tagged yet: the normal plan
		// prepares it, and every step of it is idempotent.
		if plan.Phase == PhaseNone {
			return nil, nil
		}
		return plan, nil
	}
	// The marker names a release that is already tagged: a resume finishes that
	// release and never plans a new version, so a waiting change record cannot be
	// consumed by a resume.
	releases := markerReleasePlansFor(d.Cfg, marker, configuredNames(d.Cfg), configuredResolver(d.Cfg))
	if len(releases) == 0 {
		return nil, nil
	}

	// Rebuild the publish plan from the marker: the packages, versions and tags
	// of the interrupted release, not of whatever the current tree would plan.
	plan.Phase = PhasePublish
	plan.Resumed = true
	plan.Releases = releases
	plan.Packages = markerPackages(marker.Packages)
	plan.Changes = markerChanges(marker.Changes)
	plan.Channel = marker.Channel
	plan.Marker = marker
	plan.Fingerprint = marker.Fingerprint
	if marker.Version != "" {
		plan.Version.Next = marker.Version
	}
	if len(releases) == 1 {
		plan.Tag = releases[0].Tag
		plan.PreviousTag = releases[0].PrevTag
	}
	if err := resumeAssets(ctx, d, plan); err != nil {
		return nil, err
	}
	plan.Steps = stepsFor(plan.Phase, d.Cfg.Release.Mode, plan.Assets != nil || d.Options.AssetsDir != "")
	return plan, nil
}

// resumeAssets replans the release assets so a resumed publish can still attach
// the files the original run would have built.
func resumeAssets(ctx context.Context, d *Deps, plan *Plan) error {
	if !d.Cfg.Release.Assets.Requested() && d.Options.AssetsDir == "" {
		return nil
	}
	targets, err := resumeTargets(ctx, d)
	if err != nil {
		return err
	}
	assetPlan, warnings, err := planAssets(d, targets)
	if err != nil {
		return err
	}
	plan.Assets = assetPlan
	plan.Warnings = append(plan.Warnings, warnings...)
	return nil
}

// resumeTargets inspects the registered packages so the asset planner and the
// tag version lookup work during a resume.
func resumeTargets(ctx context.Context, d *Deps) ([]target, error) {
	registered := d.Cfg.Packages
	if len(registered) == 0 {
		return nil, nil
	}
	inspected, err := inspectConfigured(d.Root, registered)
	if err != nil {
		return nil, err
	}
	return resolveTargets(ctx, d, inspected, registered)
}

// configuredNames lists the registered package identities in a stable order.
func configuredNames(cfg *config.Config) []string {
	return cfg.PackageNames()
}

// configuredResolver returns the resolver of the first registered package, used
// to spell the tag of a marker written before releases carried one.
func configuredResolver(cfg *config.Config) string {
	names := cfg.PackageNames()
	if len(names) == 0 {
		return ""
	}
	return cfg.Packages[names[0]].Resolver
}
