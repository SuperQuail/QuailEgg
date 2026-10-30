package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

func writeAssetFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMain(t *testing.T, root, dir string) {
	t.Helper()
	writeAssetFile(t, filepath.Join(root, filepath.FromSlash(dir), "main.go"), "package main\n\nfunc main() {}\n")
}

func planAssets(t *testing.T, root string, opts ecosystem.AssetOptions) (*ecosystem.AssetPlan, []string, error) {
	t.Helper()
	return (driver{}).PlanAssets(root, ecosystem.Package{Name: "demo", Path: ".", Resolver: "golang"}, opts)
}

func TestPlanAssetsCmdMain(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	plan, warnings, err := planAssets(t, root, ecosystem.AssetOptions{})
	if err != nil {
		t.Fatalf("PlanAssets: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if plan == nil || plan.Main != "cmd/demo" || plan.Name != "demo" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(plan.Targets) != len(DefaultTargets) {
		t.Fatalf("unexpected targets: %+v", plan.Targets)
	}
	if len(plan.Ldflags) != 1 || !strings.Contains(plan.Ldflags[0], "{{version}}") {
		t.Fatalf("unexpected ldflags: %v", plan.Ldflags)
	}
}

func TestPlanAssetsRootMain(t *testing.T) {
	root := t.TempDir()
	writeAssetFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	plan, _, err := planAssets(t, root, ecosystem.AssetOptions{})
	if err != nil {
		t.Fatalf("PlanAssets: %v", err)
	}
	if plan == nil || plan.Main != "." {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.Name != filepath.Base(root) {
		t.Fatalf("name = %q, want %q", plan.Name, filepath.Base(root))
	}
}

func TestPlanAssetsLibraryHasNoAssets(t *testing.T) {
	root := t.TempDir()
	writeAssetFile(t, filepath.Join(root, "demo.go"), "package demo\n\nfunc Demo() {}\n")
	plan, warnings, err := planAssets(t, root, ecosystem.AssetOptions{})
	if err != nil {
		t.Fatalf("PlanAssets: %v", err)
	}
	if plan != nil || len(warnings) != 0 {
		t.Fatalf("unexpected result: plan=%+v warnings=%v", plan, warnings)
	}
}

func TestPlanAssetsMultipleMainsWarns(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/one")
	writeMain(t, root, "cmd/two")
	plan, warnings, err := planAssets(t, root, ecosystem.AssetOptions{Targets: []string{"linux/amd64"}})
	if err != nil {
		t.Fatalf("PlanAssets: %v", err)
	}
	if plan != nil {
		t.Fatalf("expected no assets, got %+v", plan)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %v", warnings)
	}
}

func TestPlanAssetsExplicitMainMissing(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	if _, _, err := planAssets(t, root, ecosystem.AssetOptions{Main: "cmd/missing"}); err == nil {
		t.Fatal("expected an error for a missing main package")
	}
}

func TestPlanAssetsMainCannotEscape(t *testing.T) {
	root := t.TempDir()
	if _, _, err := planAssets(t, root, ecosystem.AssetOptions{Main: "../outside"}); err == nil {
		t.Fatal("expected an error for a path outside the repository")
	}
}

func TestParseTargets(t *testing.T) {
	def, err := ParseTargets(nil)
	if err != nil || len(def) != len(DefaultTargets) {
		t.Fatalf("default targets = %v, %v", def, err)
	}
	custom, err := ParseTargets([]string{"linux/arm64", " linux/arm64 ", "windows/amd64"})
	if err != nil {
		t.Fatalf("ParseTargets: %v", err)
	}
	if len(custom) != 2 || custom[0].String() != "linux/arm64" || custom[1].String() != "windows/amd64" {
		t.Fatalf("unexpected targets: %+v", custom)
	}
	if _, err := ParseTargets([]string{"linux"}); err == nil {
		t.Fatal("expected an error for a malformed target")
	}
}

func TestUnknownPlaceholderRejected(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	if _, _, err := planAssets(t, root, ecosystem.AssetOptions{Ldflags: []string{"-X main.commit={{commit}}"}}); err == nil {
		t.Fatal("expected an error for an unknown placeholder")
	}
}

func TestPlanAssetsExplicitName(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	plan, _, err := planAssets(t, root, ecosystem.AssetOptions{Name: "custom"})
	if err != nil {
		t.Fatalf("PlanAssets: %v", err)
	}
	if plan == nil || plan.Name != "custom" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}
