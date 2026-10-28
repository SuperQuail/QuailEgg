package release

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

const demoMain = "package main\n\nimport \"fmt\"\n\nvar version = \"dev\"\n\nfunc main() { fmt.Println(version) }\n"

func addGoMain(t *testing.T, dir string) {
	t.Helper()
	writeGoFile(t, filepath.Join(dir, "cmd", "demo", "main.go"), demoMain)
}

func hasStep(plan *Plan, id string) bool {
	for _, step := range plan.Steps {
		if step.ID == id {
			return true
		}
	}
	return false
}

func hostTarget() string { return runtime.GOOS + "/" + runtime.GOARCH }

func TestPlanIncludesAssets(t *testing.T) {
	dir := newGoRepo(t)
	addGoMain(t, dir)
	writeGoFile(t, filepath.Join(dir, ".changes", "add-feature.md"), "---\ntype: feat\npackages:\n  example.com/demo: minor\n---\n\nAdd a feature.\n")
	d := goDeps(t, dir)
	d.Cfg.Release.Assets.Targets = []string{hostTarget()}

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Assets == nil {
		t.Fatal("expected release assets in the plan")
	}
	if plan.Assets.Name != "demo" || plan.Assets.Main != "cmd/demo" {
		t.Fatalf("unexpected asset plan: %+v", plan.Assets)
	}
	if len(plan.Assets.Targets) != 1 || plan.Assets.Targets[0].String() != hostTarget() {
		t.Fatalf("unexpected targets: %+v", plan.Assets.Targets)
	}
	if !hasStep(plan, "assets") {
		t.Fatalf("steps are missing the assets step: %+v", plan.Steps)
	}
}

func TestLibraryHasNoAssets(t *testing.T) {
	dir := newGoRepo(t)
	d := goDeps(t, dir)
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Assets != nil {
		t.Fatalf("a library must not build assets: %+v", plan.Assets)
	}
}

func TestAssetsDisabledByConfig(t *testing.T) {
	dir := newGoRepo(t)
	addGoMain(t, dir)
	disabled := false
	d := goDeps(t, dir)
	d.Cfg.Release.Assets.Enabled = &disabled
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Assets != nil {
		t.Fatalf("disabled assets must stay off: %+v", plan.Assets)
	}
}

func TestRunBuildsAssets(t *testing.T) {
	dir := newGoRepo(t)
	addGoMain(t, dir)
	commitGoRecord(t, dir, "add-feature.md", "minor")
	d := goDeps(t, dir)
	d.Cfg.Release.Assets.Targets = []string{hostTarget()}
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assetsDir := filepath.Join(dir, ".quailegg", "assets", "v0.1.0")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatalf("assets dir: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 2 {
		t.Fatalf("expected one archive and checksums, got %v", names)
	}
	prefix := "demo_0.1.0_" + strings.ReplaceAll(hostTarget(), "/", "_")
	found := false
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			found = true
		}
	}
	if !found {
		t.Fatalf("archive %s is missing from %v", prefix, names)
	}
	checksums, err := os.ReadFile(filepath.Join(assetsDir, "checksums.txt"))
	if err != nil {
		t.Fatalf("checksums: %v", err)
	}
	if !strings.Contains(string(checksums), prefix) {
		t.Fatalf("checksums do not cover the archive:\n%s", checksums)
	}

	last, err := state.LatestRun(dir)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if last == nil || len(last.Assets) != 2 {
		t.Fatalf("run record does not list the assets: %+v", last)
	}
	clean, err := d.Git.IsCleanExcept(ctx, state.RuntimePaths())
	if err != nil {
		t.Fatalf("IsCleanExcept: %v", err)
	}
	if !clean {
		t.Fatal("the assets dir must not make the working tree dirty")
	}
}

func TestNoAssetsFlagSkipsBuild(t *testing.T) {
	dir := newGoRepo(t)
	addGoMain(t, dir)
	commitGoRecord(t, dir, "add-feature.md", "minor")
	d := goDeps(t, dir)
	d.Cfg.Release.Assets.Targets = []string{hostTarget()}
	d.Options.NoAssets = true
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".quailegg", "assets", "v0.1.0")); !os.IsNotExist(err) {
		t.Fatal("--no-assets must not create the assets dir")
	}
	exists, err := d.Git.TagExists(ctx, "v0.1.0")
	if err != nil || !exists {
		t.Fatalf("tag v0.1.0 missing: %v", err)
	}
}
