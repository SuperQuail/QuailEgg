package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

// newTauriRepo builds a workspace shaped like a Tauri app: a Cargo workspace
// whose crates inherit one version, a pnpm frontend and a Tauri configuration
// that carries the app version as well.
func newTauriRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	files := map[string]string{
		"Cargo.toml": `[workspace]
members = ["crates/core"]

[workspace.package]
version = "0.1.0"
edition = "2021"
`,
		"crates/core/Cargo.toml": `[package]
name = "sc2-core"
version.workspace = true
`,
		"ui/package.json": `{
  "name": "sc2translater-ui",
  "version": "0.1.0",
  "private": true
}
`,
		"src-tauri/tauri.conf.json": `{
  "productName": "SC2Translater",
  "version": "0.1.0",
  "bundle": {
    "active": true
  }
}
`,
		".changes/README.md":   "# Change records\n",
		".changes/core-fix.md": "---\ntype: fix\npackages:\n  sc2-core: patch\n---\n\nFix a core bug.\n",
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
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial workspace")
	return dir
}

func tauriDeps(t *testing.T, dir string) *Deps {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	repo, err := git.Open(context.Background(), dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	publish := false
	cfg.Packages = map[string]config.Package{
		"sc2-core":         {Path: "crates/core", Resolver: "rust", Publish: &publish},
		"sc2translater-ui": {Path: "ui", Resolver: "npm", Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): bump versions"
	cfg.Verify.Commands = nil
	cfg.Version.File = []config.VersionFile{{Path: "src-tauri/tauri.conf.json", Format: "json", Key: "version"}}
	return &Deps{
		Root: dir,
		Cfg:  cfg,
		Git:  repo,
		Run:  execx.OSRunner{},
		UI:   ui.Discard(),
		Now:  time.Now,
		Options: Options{
			SkipVerify: true, NoPublish: true, NoPush: true, NoGitHubRelease: true,
		},
	}
}

// TestMixedSharedReleaseBumpsVersionFile covers the Tauri layout: one shared
// version across a Cargo workspace and a frontend package, written into the
// Tauri configuration by the release itself.
func TestMixedSharedReleaseBumpsVersionFile(t *testing.T) {
	dir := newTauriRepo(t)
	d := tauriDeps(t, dir)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want version", plan.Phase)
	}
	if len(plan.Releases) != 1 {
		t.Fatalf("releases = %+v", plan.Releases)
	}
	release := plan.Releases[0]
	if release.Version != "0.1.1" || release.Tag != "v0.1.1" {
		t.Fatalf("release = %+v", release)
	}
	if len(release.Packages) != 2 {
		t.Fatalf("release packages = %v", release.Packages)
	}
	if len(plan.Packages) != 2 {
		t.Fatalf("packages = %+v", plan.Packages)
	}
	for _, pkg := range plan.Packages {
		if pkg.Next != "0.1.1" {
			t.Fatalf("package %s next = %q", pkg.Name, pkg.Next)
		}
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertFileContains(t, dir, "Cargo.toml", `version = "0.1.1"`)
	assertFileContains(t, dir, "ui/package.json", `"version": "0.1.1"`)
	assertFileContains(t, dir, "src-tauri/tauri.conf.json", `"version": "0.1.1"`)
	assertFileContains(t, dir, "src-tauri/tauri.conf.json", `"productName": "SC2Translater"`)
	assertFileContains(t, dir, "src-tauri/tauri.conf.json", `"active": true`)
	assertFileMissing(t, dir, "src-tauri/tauri.conf.json", `"version": "0.1.0"`)

	if !hasTag(t, dir, "v0.1.1") {
		t.Fatal("shared tag missing")
	}
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--name-only").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	commitFiles := string(out)
	for _, want := range []string{"ui/package.json", "src-tauri/tauri.conf.json", "Cargo.toml"} {
		if !strings.Contains(commitFiles, want) {
			t.Fatalf("release commit is missing %s:\n%s", want, commitFiles)
		}
	}
}

func assertFileContains(t *testing.T, root, rel, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s is missing %q:\n%s", rel, want, data)
	}
}

func assertFileMissing(t *testing.T, root, rel, text string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), text) {
		t.Fatalf("%s still contains %q:\n%s", rel, text, data)
	}
}
