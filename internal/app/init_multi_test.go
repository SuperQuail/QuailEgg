package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
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

func newCargoWorkspace(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "init@example.com")
	runGit(t, dir, "config", "user.name", "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/acme/demo")
	files := map[string]string{
		"Cargo.toml": `[workspace]
members = ["crates/*", "app"]

[workspace.package]
version = "0.3.0"
edition = "2021"
`,
		"crates/core/Cargo.toml": `[package]
name = "init-core"
version.workspace = true
`,
		"crates/cli/Cargo.toml": `[package]
name = "init-cli"
version.workspace = true

[dependencies]
init-core = { path = "../core" }
`,
		"app/Cargo.toml": `[package]
name = "init-app"
version.workspace = true
publish = false
`,
	}
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "chore: initial")
	return dir
}

func TestInitRegistersCargoWorkspace(t *testing.T) {
	dir := newCargoWorkspace(t)
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
	text := string(data)
	for _, want := range []string{
		`version_scope = "shared"`,
		`registries = ["crates"]`,
		`commands = ["cargo test"]`,
		`[packages."init-core"]`,
		`[packages."init-app"]`,
		`[packages."init-app"]
path = "app"
resolver = "rust"
publish = false`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated config is missing %q:\n%s", want, text)
		}
	}
}

func newInitRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "init@example.com")
	runGit(t, dir, "config", "user.name", "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/acme/demo")
	return dir
}

func writeInitFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "chore: initial")
}

func TestInitCollectsVerifyCommandsPerEcosystem(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml":             "[workspace]\nmembers = [\"crates/core\"]\n\n[workspace.package]\nversion = \"0.1.0\"\n",
		"crates/core/Cargo.toml": "[package]\nname = \"mixed-core\"\nversion.workspace = true\n",
		"package.json":           "{\"name\":\"mixed-ui\",\"scripts\":{\"test\":\"vitest run\"}}\n",
	})
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
	if !strings.Contains(string(data), "commands = [\"npm test\", \"cargo test\"]") {
		t.Fatalf("verify commands are not collected per ecosystem:\n%s", data)
	}
}

func TestInitRejectsUnknownRegistry(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml": "[package]\nname = \"solo\"\nversion = \"0.1.0\"\n",
	})
	_, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		QeggVersion:   "latest",
		Registries:    []string{"bogus"},
	})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want an unknown registry error", err)
	}
}

func TestInitRejectsUnknownResolver(t *testing.T) {
	dir := newInitRepo(t)
	cfg := "schema_version = 1\n\n[project]\ndefault_branch = \"main\"\n\n[release]\nmode = \"pr\"\nbranch = \"release\"\n\n[packages.x]\npath = \".\"\nresolver = \"bogus\"\n"
	writeInitFiles(t, dir, map[string]string{".quailegg.toml": cfg})
	_, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   false,
		QeggVersion:   "latest",
	})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want an unknown resolver error", err)
	}
}

func TestInitRejectsSharedTagVersioned(t *testing.T) {
	dir := newInitRepo(t)
	cfg := "schema_version = 1\n\n[project]\ndefault_branch = \"main\"\n\n[release]\nmode = \"pr\"\nbranch = \"release\"\nversion_scope = \"shared\"\n\n[packages.a]\npath = \".\"\nresolver = \"go\"\n\n[packages.b]\npath = \"b\"\nresolver = \"go\"\n"
	writeInitFiles(t, dir, map[string]string{".quailegg.toml": cfg})
	_, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   false,
		QeggVersion:   "latest",
	})
	if err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("err = %v, want a shared/tag-versioned error", err)
	}
}

func TestInitKeepsPrivatePackageOutOfRegistries(t *testing.T) {
	dir := newInitRepo(t)
	cfg := "schema_version = 1\n\n[project]\ndefault_branch = \"main\"\n\n[release]\nmode = \"pr\"\nbranch = \"release\"\n\n[packages.\"ui-pkg\"]\npath = \"ui\"\nresolver = \"npm\"\npublish = false\n"
	writeInitFiles(t, dir, map[string]string{
		"ui-pkg.toml": cfg,
		"Cargo.toml":  "[package]\nname = \"solo\"\nversion = \"0.1.0\"\n",
	})
	if err := os.Rename(filepath.Join(dir, "ui-pkg.toml"), filepath.Join(dir, ".quailegg.toml")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "chore: config")
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
	text := string(data)
	if strings.Contains(text, `registries = ["crates", "npm"]`) || strings.Contains(text, `registries = ["npm"]`) {
		t.Fatalf("a private package must not add npm to the registries:\n%s", text)
	}
	if !strings.Contains(text, `[packages."ui-pkg"]`) {
		t.Fatalf("the private package was dropped:\n%s", text)
	}
}

// A Tauri app keeps the released version in tauri.conf.json as well. init
// registers that file so the release writes it together with the manifest.
func TestInitRegistersTauriVersionFile(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml":                "[package]\nname = \"sc2translater\"\nversion = \"0.1.0\"\n",
		"src-tauri/tauri.conf.json": "{\n  \"productName\": \"SC2Translater\",\n  \"version\": \"0.1.0\"\n}\n",
	})
	opts := InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		QeggVersion:   "latest",
	}
	if _, err := Init(context.Background(), dir, ui.Discard(), opts); err != nil {
		t.Fatalf("Init: %v", err)
	}
	configPath := filepath.Join(dir, ".quailegg.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[[version.file]]") || !strings.Contains(text, `path = "src-tauri/tauri.conf.json"`) {
		t.Fatalf("the tauri configuration was not registered:\n%s", text)
	}
	// Running init again keeps the file registered exactly once.
	if _, err := Init(context.Background(), dir, ui.Discard(), opts); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "[[version.file]]"); count != 1 {
		t.Fatalf("the configuration lists the version file %d times:\n%s", count, data)
	}
}

func TestInferRegistriesSkipsPrivatePackages(t *testing.T) {
	publish := false
	packages := map[string]config.Package{
		"ui-pkg": {Path: "ui", Resolver: "npm", Publish: &publish},
		"solo":   {Path: ".", Resolver: "rust"},
	}
	if got := inferRegistries(packages); len(got) != 1 || got[0] != "crates" {
		t.Fatalf("inferRegistries = %v, want only crates", got)
	}
}
