package app

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// Resume finishes the recorded release. A run whose publish finished and whose
// GitHub Release creation failed is repaired in place; every other recorded
// release is continued by the publish phase, which decides per package and per
// step what is still missing and never plans a new version while the marker
// still describes a tagged release.
func (a *App) Resume(ctx context.Context, opts release.Options) (*release.Plan, error) {
	latest, err := state.LatestRun(a.Root)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		a.UI.Info("%s", i18n.Tf("resume.previous", latest.RunID, latest.Phase, latest.Status))
	}
	deps := a.deps(opts)
	if needsReleaseRepair(latest) {
		plan, handled, err := release.Repair(ctx, deps)
		if err != nil {
			return nil, err
		}
		if handled {
			return plan, nil
		}
	}
	plan, err := release.ResumePlan(ctx, deps)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		a.UI.Info("%s", i18n.T("resume.nothing"))
		return nil, nil
	}
	return plan, release.Run(ctx, deps, plan)
}

// needsReleaseRepair reports whether the recorded run failed at the GitHub
// Release and nothing else. That is the one state a repair answers for; a
// missing run record or an unfinished publish belongs to the publish phase.
func needsReleaseRepair(run *state.RunRecord) bool {
	if run == nil {
		return false
	}
	if run.Steps["github-release-repair"].Status == state.StatusDone {
		// An earlier resume already repaired this release; let it report the
		// outcome instead of touching the remote again.
		return true
	}
	if run.Steps["github-release"].Status != state.StatusFailed {
		return false
	}
	for _, step := range []string{"publish", "tag", "push"} {
		recorded, ok := run.Steps[step]
		if ok && recorded.Status != state.StatusDone && recorded.Status != state.StatusSkipped {
			return false
		}
	}
	return true
}
