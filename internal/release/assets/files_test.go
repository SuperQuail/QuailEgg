package assets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func writeCollectFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCollectsConfiguredFiles(t *testing.T) {
	root := t.TempDir()
	writeCollectFile(t, root, "dist/app_linux_amd64.tar.gz", "linux")
	writeCollectFile(t, root, "dist/app_windows_amd64.zip", "windows")
	writeCollectFile(t, root, "dist/notes.txt", "ignored")
	outDir := filepath.Join(t.TempDir(), "assets")
	plan := &Plan{Name: "app", Files: []string{"dist/*.tar.gz", "dist/*.zip"}}
	files, err := plan.Build(context.Background(), execx.OSRunner{}, root, "v0.1.0", outDir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected two payloads and checksums, got %v", files)
	}
	if filepath.Base(files[2]) != "checksums.txt" {
		t.Fatalf("checksums must be uploaded last: %v", files)
	}
	data, err := os.ReadFile(files[2])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app_linux_amd64.tar.gz", "app_windows_amd64.zip"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("checksums.txt is missing %s:\n%s", want, data)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "notes.txt")); !os.IsNotExist(err) {
		t.Fatal("a file outside the patterns was collected")
	}
}

func TestBuildMatchesDoubleStar(t *testing.T) {
	root := t.TempDir()
	writeCollectFile(t, root, "target/release/bundle/msi/app.msi", "x")
	writeCollectFile(t, root, "target/release/bundle/deb/app.deb", "y")
	writeCollectFile(t, root, "target/debug/app.msi", "z")
	outDir := filepath.Join(t.TempDir(), "assets")
	plan := &Plan{Name: "app", Files: []string{"target/release/**/*.msi", "target/release/**/*.deb"}}
	files, err := plan.Build(context.Background(), execx.OSRunner{}, root, "1.0.0", outDir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected two bundles and checksums, got %v", files)
	}
	for _, want := range []string{"app.msi", "app.deb"} {
		if _, err := os.Stat(filepath.Join(outDir, want)); err != nil {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
}

func TestBuildWithoutMatchFails(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{Name: "app", Files: []string{"dist/*.zip"}}
	_, err := plan.Build(context.Background(), execx.OSRunner{}, root, "1.0.0", filepath.Join(t.TempDir(), "assets"))
	if err == nil || !strings.Contains(err.Error(), "dist/*.zip") {
		t.Fatalf("expected a no-match error, got %v", err)
	}
}

func TestBuildRejectsDuplicateNames(t *testing.T) {
	root := t.TempDir()
	writeCollectFile(t, root, "one/app.zip", "a")
	writeCollectFile(t, root, "two/app.zip", "b")
	plan := &Plan{Name: "app", Files: []string{"one/*.zip", "two/*.zip"}}
	_, err := plan.Build(context.Background(), execx.OSRunner{}, root, "1.0.0", filepath.Join(t.TempDir(), "assets"))
	if err == nil || !strings.Contains(err.Error(), "app.zip") {
		t.Fatalf("expected a duplicate error, got %v", err)
	}
}

func TestValidateRequiresFilesForCommands(t *testing.T) {
	plan := &Plan{Commands: []string{"make release"}}
	if err := plan.Validate(); err == nil {
		t.Fatal("commands without files must be rejected")
	}
}

func TestValidateRejectsEscapingFiles(t *testing.T) {
	plan := &Plan{Files: []string{"../outside.zip"}}
	if err := plan.Validate(); err == nil {
		t.Fatal("a glob outside the repository must be rejected")
	}
}

// commandRunner records the commands and writes the file they build.
type commandRunner struct {
	calls []string
	envs  []string
}

func (c *commandRunner) Run(ctx context.Context, dir, name string, args ...string) (execx.Result, error) {
	return c.RunEnv(ctx, dir, nil, name, args...)
}

func (c *commandRunner) RunEnv(_ context.Context, dir string, env []string, name string, args ...string) (execx.Result, error) {
	c.calls = append(c.calls, name+" "+strings.Join(args, " "))
	c.envs = append(c.envs, env...)
	write := filepath.Join(dir, "dist", "app.zip")
	if err := os.MkdirAll(filepath.Dir(write), 0o755); err != nil {
		return execx.Result{}, err
	}
	return execx.Result{}, os.WriteFile(write, []byte("bundle"), 0o644)
}

func TestBuildRunsCommands(t *testing.T) {
	root := t.TempDir()
	runner := &commandRunner{}
	plan := &Plan{Name: "app", Commands: []string{"make release {{version}}"}, Files: []string{"dist/*.zip"}}
	files, err := plan.Build(context.Background(), runner, root, "v1.2.3", filepath.Join(t.TempDir(), "assets"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected payload and checksums, got %v", files)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "1.2.3") {
		t.Fatalf("version placeholder was not expanded: %v", runner.calls)
	}
	if !containsEnv(runner.envs, "QEGG_VERSION=1.2.3") || !containsEnv(runner.envs, "QEGG_TAG=v1.2.3") {
		t.Fatalf("release env was not injected: %v", runner.envs)
	}
}

func TestExpandLdflags(t *testing.T) {
	expanded := expand([]string{"-X main.version={{version}}", "-X main.tag={{tag}}"}, "v1.2.3")
	if expanded[0] != "-X main.version=1.2.3" || expanded[1] != "-X main.tag=v1.2.3" {
		t.Fatalf("unexpected ldflags: %v", expanded)
	}
}

func TestMatchPatternDoubleStar(t *testing.T) {
	tests := map[string]bool{
		"target/release/**/*.msi": true,
		"target/debug/app.msi":    false,
		"**/*.dmg":                true,
	}
	for pattern, want := range tests {
		rel := "target/release/bundle/msi/app.msi"
		if pattern == "**/*.dmg" {
			rel = "bundle/app.dmg"
		}
		if got := matchPattern(pattern, rel); got != want {
			t.Fatalf("matchPattern(%q, %q) = %v, want %v", pattern, rel, got, want)
		}
	}
}
func TestCollectPrebuiltDirectory(t *testing.T) {
	dir := t.TempDir()
	writeCollectFile(t, dir, "windows/app.msi", "win")
	writeCollectFile(t, dir, "linux/app.deb", "linux")
	outDir := filepath.Join(t.TempDir(), "assets")
	files, err := Collect(dir, outDir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected two payloads and checksums, got %v", files)
	}
	if filepath.Base(files[0]) != "app.deb" || filepath.Base(files[1]) != "app.msi" {
		t.Fatalf("unexpected upload order: %v", files)
	}
}

func TestCollectRejectsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	writeCollectFile(t, dir, "windows/app.msi", "win")
	writeCollectFile(t, dir, "linux/app.msi", "linux")
	if _, err := Collect(dir, filepath.Join(t.TempDir(), "assets")); err == nil {
		t.Fatal("expected a duplicate error")
	}
}

func TestCollectRejectsEmptyDirectory(t *testing.T) {
	if _, err := Collect(t.TempDir(), filepath.Join(t.TempDir(), "assets")); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}
