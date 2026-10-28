package app

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/release"
)

// Plan builds the release plan without side effects.
func (a *App) Plan(ctx context.Context) (*release.Plan, error) {
	return release.Build(ctx, a.deps(release.Options{}))
}
