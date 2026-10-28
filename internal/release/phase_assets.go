package release

import (
	"context"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// buildAssets cross-compiles and packages the release binaries. It runs before
// the registry publish and the tag so a broken build stops the release early.
func (e *executor) buildAssets(ctx context.Context) error {
	if e.d.Options.NoAssets {
		e.skip("assets", i18n.T("release.skip.no_assets"))
		return nil
	}
	if e.plan.Assets == nil {
		return nil
	}
	outDir := filepath.Join(e.d.Root, ".quailegg", "assets", e.plan.Tag)
	files, err := e.plan.Assets.Build(ctx, e.d.Run, e.d.Root, e.plan.Version.Next, outDir)
	if err != nil {
		return err
	}
	e.run.Assets = files
	e.d.UI.Success("%s", i18n.Tf("release.msg.assets_built", len(files), outDir))
	return nil
}
