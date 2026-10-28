package app

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release"
)

// Release executes the plan derived from the current repository state.
func (a *App) Release(ctx context.Context, opts release.Options) (*release.Plan, error) {
	plan, err := release.Build(ctx, a.deps(opts))
	if err != nil {
		return nil, err
	}
	if plan.Phase == release.PhaseNone {
		a.UI.Info("%s", i18n.T("release.nothing"))
		return plan, nil
	}
	return plan, release.Run(ctx, a.deps(opts), plan)
}
