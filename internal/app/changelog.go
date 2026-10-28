package app

import (
	"context"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/changelog"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release"
)

// Changelog writes (or previews) the changelog section for the pending release.
func (a *App) Changelog(ctx context.Context, dryRun bool) error {
	plan, err := a.Plan(ctx)
	if err != nil {
		return err
	}
	if plan.Phase != release.PhaseVersion {
		a.UI.Info("%s", i18n.T("changelog.nothing"))
		return nil
	}
	section := release.ChangelogSection(plan, a.Now())
	rendered := changelog.Render(section)
	if dryRun {
		a.UI.Info("%s", rendered)
		return nil
	}
	path := filepath.Join(a.Root, "CHANGELOG.md")
	existing, err := fsx.ReadOrEmpty(path)
	if err != nil {
		return err
	}
	updated, err := changelog.Update(existing, section)
	if err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(path, updated, 0o644); err != nil {
		return err
	}
	a.UI.Success("%s", i18n.Tf("changelog.updated", plan.Version.Next))
	return nil
}
