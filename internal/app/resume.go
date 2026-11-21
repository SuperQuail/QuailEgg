package app

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// Resume finishes the recorded release. A run that lost only its GitHub Release
// is repaired in place; every other state plans the release again, which is safe
// because each step is idempotent.
func (a *App) Resume(ctx context.Context, opts release.Options) (*release.Plan, error) {
	latest, err := state.LatestRun(a.Root)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		a.UI.Info("%s", i18n.Tf("resume.previous", latest.RunID, latest.Phase, latest.Status))
	}
	plan, handled, err := release.Repair(ctx, a.deps(opts))
	if err != nil {
		return nil, err
	}
	if handled {
		return plan, nil
	}
	return a.Release(ctx, opts)
}
