package release

import (
	"context"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/assets"
)

// buildAssets produces the files attached to the release. A prebuilt directory
// wins (the managed workflow downloads the platform builds into it), then the
// configured commands or the ecosystem build run locally.
func (e *executor) buildAssets(ctx context.Context) error {
	if e.d.Options.NoAssets {
		e.skip("assets", i18n.T("release.skip.no_assets"))
		return nil
	}
	if e.plan.Assets == nil && e.d.Options.AssetsDir == "" {
		return nil
	}
	outDir := filepath.Join(e.d.Root, ".quailegg", "assets", e.plan.Tag)
	var files []string
	var err error
	if e.d.Options.AssetsDir != "" {
		dir := e.d.Options.AssetsDir
		if !filepath.IsAbs(dir) {
			dir = fsx.Join(e.d.Root, filepath.ToSlash(dir))
		}
		files, err = assets.Collect(dir, outDir)
	} else {
		files, err = e.plan.Assets.Build(ctx, e.d.Run, e.d.Root, e.plan.Version.Next, outDir)
	}
	if err != nil {
		return err
	}
	e.run.Assets = files
	e.d.UI.Success("%s", i18n.Tf("release.msg.assets_built", len(files), outDir))
	return nil
}
