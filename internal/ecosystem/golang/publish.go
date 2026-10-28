package golang

import (
	"context"
	"net/http"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

type publisher struct{}

func (publisher) Name() string { return "go" }

// Precheck asks the Go module proxy whether the version is already cached. The
// proxy fetches versions on demand, so a 404 only means "not cached yet";
// publishing a module is the pushed release tag itself.
func (publisher) Precheck(ctx context.Context, client *http.Client, pkg ecosystem.Package, releaseVersion string) (ecosystem.Status, error) {
	return ecosystem.CheckURL(ctx, client, endpoint(pkg.Name, releaseVersion))
}

func (publisher) Build(context.Context, execx.Runner, string, ecosystem.Package) error { return nil }

// Publish is a no-op: the module becomes available once the release tag is
// pushed, which the release pipeline does in its tag and push steps.
func (publisher) Publish(context.Context, execx.Runner, string, ecosystem.Package, string) error {
	return nil
}

func endpoint(modulePath, version string) string {
	return "https://proxy.golang.org/" + escapePath(modulePath) + "/@v/" + escapePath(version) + ".info"
}

// escapePath applies the Go module proxy encoding: every uppercase letter is
// replaced by '!' followed by its lowercase form.
func escapePath(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
