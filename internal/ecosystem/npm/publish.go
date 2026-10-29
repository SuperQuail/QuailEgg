package npm

import (
	"context"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/semver"
	"net/http"
	"net/url"
	"strings"
)

type publisher struct{}

func (publisher) Name() string { return "npm" }

func (publisher) Precheck(ctx context.Context, client *http.Client, pkg ecosystem.Package, releaseVersion string) (ecosystem.Status, error) {
	return ecosystem.CheckURL(ctx, client, endpoint(pkg.Name, releaseVersion))
}

func (publisher) Build(context.Context, execx.Runner, string, ecosystem.Package) error {
	return nil
}

// Publish uploads the package. Prerelease versions go to a dist-tag named
// after their channel so they never replace the latest release.
func (publisher) Publish(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, version string) error {
	dir := fsx.Join(root, pkg.Path)
	args := []string{"publish"}
	if _, channel, _, ok := semver.SplitPrerelease(version); ok {
		args = append(args, "--tag", channel)
	}
	_, err := run.Run(ctx, dir, "npm", args...)
	return err
}

func endpoint(name, releaseVersion string) string {
	escaped := url.PathEscape(name)
	escaped = strings.ReplaceAll(escaped, "%40", "@")
	return "https://registry.npmjs.org/" + escaped + "/" + url.PathEscape(releaseVersion)
}
