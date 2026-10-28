package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func withEnabled() Options {
	enabled := true
	return Options{Enabled: &enabled}
}

func TestDetectOffByDefault(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	plan, warnings, err := Detect(root, Options{})
	if err != nil || plan != nil || len(warnings) != 0 {
		t.Fatalf("assets must be off by default: plan=%+v warnings=%v err=%v", plan, warnings, err)
	}
}

func TestDetectCmdMain(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	plan, warnings, err := Detect(root, withEnabled())
	if err != nil {
		t.Fatalf("Detect: %v", err)
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

func TestDetectRootMain(t *testing.T) {
	root := t.TempDir()
	writeAssetFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	plan, _, err := Detect(root, withEnabled())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if plan == nil || plan.Main != "." {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if plan.Name != filepath.Base(root) {
		t.Fatalf("name = %q, want %q", plan.Name, filepath.Base(root))
	}
}

func TestDetectSettingsEnableAssets(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	plan, _, err := Detect(root, Options{Targets: []string{"linux/amd64"}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if plan == nil || len(plan.Targets) != 1 {
		t.Fatalf("writing an asset setting must enable assets: %+v", plan)
	}
}

func TestDetectLibraryHasNoAssets(t *testing.T) {
	root := t.TempDir()
	writeAssetFile(t, filepath.Join(root, "demo.go"), "package demo\n\nfunc Demo() {}\n")
	plan, warnings, err := Detect(root, withEnabled())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if plan != nil || len(warnings) != 0 {
		t.Fatalf("unexpected result: plan=%+v warnings=%v", plan, warnings)
	}
}

func TestDetectMultipleMainsAuto(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/one")
	writeMain(t, root, "cmd/two")
	plan, warnings, err := Detect(root, Options{Targets: []string{"linux/amd64"}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if plan != nil {
		t.Fatalf("expected no assets, got %+v", plan)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %v", warnings)
	}
}

func TestDetectMultipleMainsExplicit(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/one")
	writeMain(t, root, "cmd/two")
	if _, _, err := Detect(root, withEnabled()); err == nil {
		t.Fatal("expected an error when assets are forced with several mains")
	}
}

func TestDetectDisabled(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	disabled := false
	plan, warnings, err := Detect(root, Options{Enabled: &disabled, Targets: []string{"linux/amd64"}})
	if err != nil || plan != nil || len(warnings) != 0 {
		t.Fatalf("unexpected result: plan=%+v warnings=%v err=%v", plan, warnings, err)
	}
}

func TestDetectExplicitMainMissing(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	if _, _, err := Detect(root, Options{Main: "cmd/missing"}); err == nil {
		t.Fatal("expected an error for a missing main package")
	}
}

func TestDetectMainCannotEscape(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Detect(root, Options{Main: "../outside"}); err == nil {
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

func TestExpandLdflags(t *testing.T) {
	expanded := expand([]string{"-X main.version={{version}}", "-X main.tag={{tag}}"}, "v1.2.3")
	if expanded[0] != "-X main.version=1.2.3" || expanded[1] != "-X main.tag=v1.2.3" {
		t.Fatalf("unexpected ldflags: %v", expanded)
	}
}

func TestUnknownPlaceholderRejected(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	if _, _, err := Detect(root, Options{Ldflags: []string{"-X main.commit={{commit}}"}}); err == nil {
		t.Fatal("expected an error for an unknown placeholder")
	}
}

func TestDetectExplicitName(t *testing.T) {
	root := t.TempDir()
	writeMain(t, root, "cmd/demo")
	plan, _, err := Detect(root, Options{Name: "custom"})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if plan == nil || plan.Name != "custom" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}
