package java

import (
	"context"
	"net/http"
	"path/filepath"
	"runtime"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type publisher struct{}

func (publisher) Name() string { return "java" }

// Precheck cannot answer without the repository URL, which lives inside the
// build file rather than in the package name. Deployment stays authoritative,
// so a Java package is never skipped because of a guess.
func (publisher) Precheck(context.Context, *http.Client, ecosystem.Package, string) (ecosystem.Status, error) {
	return ecosystem.Unknown, nil
}

// Build does nothing: the deploy tasks build the artifacts themselves.
func (publisher) Build(context.Context, execx.Runner, string, ecosystem.Package) error { return nil }

// Publish deploys the package to its configured Maven repository.
func (publisher) Publish(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, _ string) error {
	dir := fsx.Join(root, pkg.Path)
	if manifest, ok := packageManifest(root, pkg); ok && kindOf(manifest) == buildGradle {
		return publishGradle(ctx, run, dir)
	}
	return publishMaven(ctx, run, dir)
}

// publishMaven deploys with the Maven wrapper when the repository ships one.
func publishMaven(ctx context.Context, run execx.Runner, dir string) error {
	if fsx.Exists(filepath.Join(dir, "mvnw")) {
		if runtime.GOOS == "windows" {
			_, err := run.Run(ctx, dir, "cmd", "/c", "mvnw.cmd -B -DskipTests deploy")
			return err
		}
		_, err := run.Run(ctx, dir, "sh", "-c", "./mvnw -B -DskipTests deploy")
		return err
	}
	_, err := run.Run(ctx, dir, "mvn", "-B", "-DskipTests", "deploy")
	return err
}

// publishGradle publishes with the Gradle wrapper when the repository has one.
func publishGradle(ctx context.Context, run execx.Runner, dir string) error {
	if fsx.Exists(filepath.Join(dir, "gradlew")) {
		if runtime.GOOS == "windows" {
			_, err := run.Run(ctx, dir, "cmd", "/c", "gradlew.bat publish")
			return err
		}
		_, err := run.Run(ctx, dir, "sh", "-c", "./gradlew publish")
		return err
	}
	_, err := run.Run(ctx, dir, "gradle", "publish")
	return err
}
