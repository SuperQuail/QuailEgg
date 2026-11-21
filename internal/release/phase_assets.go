package release

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/assets"
)

// buildAssets produces the files attached to the release. A prebuilt directory
// wins (the managed workflow downloads the platform builds into it), then the
// configured commands or the ecosystem build run locally.
//
// Either way the files must come from the pinned release snapshot: a local build
// runs inside that snapshot's checkout, and a prebuilt directory has to name the
// same commit in its provenance manifest. An attachment built from another tree
// would be published as if it belonged to this release.
func (e *executor) buildAssets(ctx context.Context) error {
	if e.d.Options.NoAssets {
		e.skip("assets", i18n.T("release.skip.no_assets"))
		return nil
	}
	if e.plan.Assets == nil && e.d.Options.AssetsDir == "" {
		return nil
	}
	root, err := e.sourceRoot(ctx)
	if err != nil {
		return err
	}
	outDir := filepath.Join(e.d.Root, ".quailegg", "assets", e.plan.Tag)
	var files []string
	if e.d.Options.AssetsDir != "" {
		dir := e.d.Options.AssetsDir
		if !filepath.IsAbs(dir) {
			// A relative directory points into the checked-out tree: the build
			// jobs drop their artifacts there, and .quailegg/assets is ignored
			// by git, so it never exists inside the snapshot.
			dir = fsx.Join(e.d.Root, filepath.ToSlash(dir))
		}
		if err := e.verifyAssets(dir); err != nil {
			return err
		}
		files, err = assets.Collect(dir, outDir)
	} else {
		files, err = e.plan.Assets.Build(ctx, e.d.Run, root, e.plan.Version.Next, outDir)
	}
	if err != nil {
		return err
	}
	e.run.Assets = files
	e.d.UI.Success("%s", i18n.Tf("release.msg.assets_built", len(files), outDir))
	return nil
}

// verifyAssets checks that a prebuilt assets directory came from the release
// snapshot. A manifest is always checked when it is there; it becomes mandatory
// as soon as the working tree is no longer the snapshot, because nothing else
// then ties the files to the commit that is being released.
func (e *executor) verifyAssets(dir string) error {
	provenance, err := assets.LoadProvenance(dir)
	if err != nil {
		return err
	}
	if provenance == nil {
		if !e.workingTreeIsSource() {
			return errors.New(i18n.Tf("release.err.assets_provenance", assets.ProvenanceFile))
		}
		return nil
	}
	if err := provenance.Verify(dir, e.identity.SourceCommit); err != nil {
		return err
	}
	return nil
}
