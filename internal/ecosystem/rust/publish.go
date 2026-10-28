package rust

import (
	"context"
	"net/http"
	"net/url"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type publisher struct{}

func (publisher) Name() string { return "rust" }

func (publisher) Precheck(ctx context.Context, client *http.Client, pkg ecosystem.Package, releaseVersion string) (ecosystem.Status, error) {
	return ecosystem.CheckURL(ctx, client, endpoint(pkg.Name, releaseVersion))
}

func (publisher) Build(context.Context, execx.Runner, string, ecosystem.Package) error {
	return nil
}

func (publisher) Publish(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, _ string) error {
	dir := fsx.Join(root, pkg.Path)
	_, err := run.Run(ctx, dir, "cargo", "publish")
	return err
}

func endpoint(name, releaseVersion string) string {
	return "https://crates.io/api/v1/crates/" + url.PathEscape(name) + "/" + url.PathEscape(releaseVersion)
}
