package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newPythonWorkspace(t *testing.T, versions map[string]string) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "init@example.com")
	runGit(t, dir, "config", "user.name", "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/acme/demo")
	root := "[tool.uv.workspace]\nmembers = [\"packages/*\", \"app\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"app": "[project]\nname = \"app\"\nversion = \"" + versions["app"] + "\"\n", "packages/core": "[project]\nname = \"core\"\nversion = \"" + versions["core"] + "\"\n"}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel), "pyproject.toml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "chore: initial")
	return dir
}

func TestInitInfersSharedScopeAndPythonRegistry(t *testing.T) {
	dir := newPythonWorkspace(t, map[string]string{"app": "2.0.2-a14", "core": "2.0.2-a14"})
	plan, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		QeggVersion:   "latest",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(plan.Files) == 0 {
		t.Fatal("Init planned no files")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".quailegg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`version_scope = "shared"`, `registries = ["pypi"]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated config is missing %s:\n%s", want, text)
		}
	}
	for _, want := range []string{"[packages.core]", "[packages.app]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated config is missing %s:\n%s", want, text)
		}
	}
}

func TestInitKeepsPackageScopeForMixedVersions(t *testing.T) {
	dir := newPythonWorkspace(t, map[string]string{"app": "2.0.1", "core": "2.0.2"})
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".quailegg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "version_scope") {
		t.Fatalf("mixed versions must stay in package scope:\n%s", data)
	}
}
