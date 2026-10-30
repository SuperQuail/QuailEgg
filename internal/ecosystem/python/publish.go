package python

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type publisher struct{}

func (publisher) Name() string { return "python" }

func (publisher) Precheck(ctx context.Context, client *http.Client, pkg ecosystem.Package, releaseVersion string) (ecosystem.Status, error) {
	return ecosystem.CheckURL(ctx, client, endpoint(pkg.Name, releaseVersion))
}

func (publisher) Build(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package) error {
	dir := fsx.Join(root, pkg.Path)
	if execx.LookPath("uv") {
		_, err := run.Run(ctx, dir, "uv", "build")
		return err
	}
	_, err := run.Run(ctx, dir, "python", "-m", "build")
	return err
}

func (publisher) Publish(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, _ string) error {
	dir := fsx.Join(root, pkg.Path)
	if execx.LookPath("uv") {
		_, err := run.Run(ctx, dir, "uv", "publish")
		return err
	}
	distDir := filepath.Join(dir, "dist")
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return fmt.Errorf("%s: %w", distDir, err)
	}
	var artefacts []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".whl") || strings.HasSuffix(name, ".tar.gz") {
			artefacts = append(artefacts, filepath.Join(distDir, name))
		}
	}
	if len(artefacts) == 0 {
		return fmt.Errorf("%s: no build artefacts found", distDir)
	}
	args := append([]string{"upload"}, artefacts...)
	_, err = run.Run(ctx, dir, "twine", args...)
	return err
}

func endpoint(name, releaseVersion string) string {
	return "https://pypi.org/pypi/" + url.PathEscape(name) + "/" + url.PathEscape(canonicalVersion(releaseVersion)) + "/json"
}
