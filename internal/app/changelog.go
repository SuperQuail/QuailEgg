package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changelog"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release"
)

// Changelog writes (or previews) the changelog sections for the pending release.
func (a *App) Changelog(ctx context.Context, dryRun bool) error {
	plan, err := a.Plan(ctx)
	if err != nil {
		return err
	}
	if plan.Phase != release.PhaseVersion {
		a.UI.Info("%s", i18n.T("changelog.nothing"))
		return nil
	}
	sections := release.ChangelogSections(plan, a.Now())
	var rendered strings.Builder
	for _, section := range sections {
		rendered.WriteString(changelog.Render(section))
	}
	if dryRun {
		a.UI.Info("%s", rendered.String())
		return nil
	}
	path := filepath.Join(a.Root, "CHANGELOG.md")
	existing, err := fsx.ReadOrEmpty(path)
	if err != nil {
		return err
	}
	updated, err := changelog.UpdateAll(existing, sections)
	if err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(path, updated, 0o644); err != nil {
		return err
	}
	title := plan.Version.Next
	if title == "" {
		a.UI.Success("%s", i18n.Tf("changelog.updated_multi", len(sections)))
		return nil
	}
	a.UI.Success("%s", i18n.Tf("changelog.updated", title))
	return nil
}
