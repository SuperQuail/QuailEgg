package app

import (
	"context"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// Resume re-runs the release; every step is idempotent.
func (a *App) Resume(ctx context.Context, opts release.Options) (*release.Plan, error) {
	latest, err := state.LatestRun(a.Root)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		a.UI.Info("%s", i18n.Tf("resume.previous", latest.RunID, latest.Phase, latest.Status))
	}
	return a.Release(ctx, opts)
}
