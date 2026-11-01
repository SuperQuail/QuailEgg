package java

import (
	"context"
	"net/http"
	"path"
	"runtime"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

type publisher struct{}

func (publisher) Name() string { return "java" }

// Precheck cannot answer without the repository URL, which lives inside the
// build file rather than in the package name. Deployment stays authoritative.
func (publisher) Precheck(context.Context, *http.Client, ecosystem.Package, string) (ecosystem.Status, error) {
	return ecosystem.Unknown, nil
}

// Build does nothing: the deploy tasks build the artifacts themselves.
func (publisher) Build(context.Context, execx.Runner, string, ecosystem.Package) error { return nil }

// Publish deploys the package to its configured Maven repository.
func (publisher) Publish(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, _ string) error {
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return nil
	}
	if kindOf(manifest) == buildMaven {
		return publishMaven(ctx, run, root, pkg)
	}
	return publishGradle(ctx, run, root, pkg)
}

// publishMaven deploys one Maven package with the repository wrapper when one
// exists. An aggregator deploys only its own pom: its modules deploy themselves
// so a release never deploys the same artifact twice.
func publishMaven(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package) error {
	args := []string{"-B", "-DskipTests"}
	if reactor, err := loadMavenReactor(root); err == nil {
		if node := reactor.nodeForDir(pkg.Path); node != nil && len(node.doc.Modules) > 0 {
			args = append(args, "-N")
		}
	}
	args = append(args, "deploy")
	return runWithWrapper(ctx, run, root, pkg.Path, mavenWrapperNames(), "mvn", args)
}

// publishGradle publishes one Gradle project from the repository root when its
// project path is known, so the repository wrapper and multi-project layout are
// honoured.
func publishGradle(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package) error {
	args := []string{"publish"}
	dir := pkg.Path
	if ws, err := loadGradleWorkspace(root); err == nil {
		if task := gradleProjectTask(ws, pkg.Path); task != "" {
			args = []string{task + "publish"}
			if task != ":" {
				args = []string{task + ":publish"}
			}
			dir = "."
		}
	}
	return runWithWrapper(ctx, run, root, dir, gradleWrapperNames(), "gradle", args)
}

// gradleProjectTask returns the Gradle task path of pkg, if known.
func gradleProjectTask(ws *gradleWorkspace, dir string) string {
	dir = path.Clean(strings.TrimSpace(dir))
	if dir == "." || dir == "" {
		return ":"
	}
	for _, include := range ws.settings.includes {
		if path.Clean(include.dir) == dir {
			return include.projectPath
		}
	}
	return ""
}

// runWithWrapper runs args in dir with the nearest wrapper, falling back to
// the global command.
func runWithWrapper(ctx context.Context, run execx.Runner, root, dir string, names []string, fallback string, args []string) error {
	if wrapper, ok := findWrapper(root, dir, names...); ok {
		command := strings.Join(args, " ")
		if runtime.GOOS == "windows" {
			_, err := run.Run(ctx, fsx.Join(root, dir), "cmd", "/c", windowsQuote(wrapper)+" "+command)
			return err
		}
		_, err := run.Run(ctx, fsx.Join(root, dir), "sh", "-c", shellQuote(wrapper)+" "+command)
		return err
	}
	_, err := run.Run(ctx, fsx.Join(root, dir), fallback, args...)
	return err
}

// mavenWrapperNames returns the wrapper names of the current platform.
func mavenWrapperNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"mvnw.cmd", "mvnw"}
	}
	return []string{"mvnw"}
}

// gradleWrapperNames returns the wrapper names of the current platform.
func gradleWrapperNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"gradlew.bat", "gradlew"}
	}
	return []string{"gradlew"}
}
