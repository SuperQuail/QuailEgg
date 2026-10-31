package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

func newAliasedRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	files := map[string]string{
		"package.json":              "{\n  \"name\": \"app\",\n  \"version\": \"1.0.0\",\n  \"private\": true\n}\n",
		"src-tauri/Cargo.toml":      "[package]\nname = \"app\"\nversion = \"1.0.0\"\n",
		"src-tauri/tauri.conf.json": "{\n  \"version\": \"1.0.0\"\n}\n",
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
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial app")
	return dir
}

func aliasedDeps(t *testing.T, dir, scope string) *Deps {
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
		"npm@app":  {Name: "app", Path: ".", Resolver: "npm", Publish: &publish},
		"rust@app": {Name: "app", Path: "src-tauri", Resolver: "rust", Publish: &publish},
	}
	cfg.Release.VersionScope = scope
	if scope == config.VersionScopeShared {
		cfg.Version.File = []config.VersionFile{{Path: "src-tauri/tauri.conf.json", Format: "json", Key: "version"}}
	}
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): v{{version}}"
	cfg.Verify.Commands = nil
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

func TestAliasedSharedReleaseWritesBothManifests(t *testing.T) {
	dir := newAliasedRepo(t)
	writeRecord(t, dir, "app-feature.md", "Ship the next app version.", map[string]string{
		"npm@app": "minor", "rust@app": "minor",
	}, true)
	d := aliasedDeps(t, dir, config.VersionScopeShared)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "1.1.0" || plan.Tag != "v1.1.0" {
		t.Fatalf("plan version = %+v tag %q", plan.Version, plan.Tag)
	}
	if len(plan.Packages) != 2 {
		t.Fatalf("packages = %+v", plan.Packages)
	}
	names := map[string]string{}
	for _, pkg := range plan.Packages {
		names[pkg.Name] = pkg.Next
	}
	for _, key := range []string{"npm@app", "rust@app"} {
		if names[key] != "1.1.0" {
			t.Fatalf("package %s next = %q", key, names[key])
		}
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, rel := range []string{"package.json", "src-tauri/Cargo.toml", "src-tauri/tauri.conf.json"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "1.1.0") {
			t.Fatalf("%s was not bumped:\n%s", rel, data)
		}
	}
	if !hasTag(t, dir, "v1.1.0") {
		t.Fatal("shared tag v1.1.0 is missing")
	}
	marker, err := state.LoadMarker(dir)
	if err != nil || marker == nil {
		t.Fatalf("marker = %+v, err = %v", marker, err)
	}
	byName := map[string]bool{}
	for _, pkg := range marker.Packages {
		byName[pkg.Name] = true
	}
	for _, key := range []string{"npm@app", "rust@app"} {
		if !byName[key] {
			t.Fatalf("marker packages = %+v", marker.Packages)
		}
	}

	again, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after release: %v", err)
	}
	if again.Phase != PhaseNone {
		t.Fatalf("phase after release = %q", again.Phase)
	}
}

func TestAliasedPackageTagsUseIdentity(t *testing.T) {
	dir := newAliasedRepo(t)
	writeRecord(t, dir, "npm-fix.md", "Fix the frontend.", map[string]string{"npm@app": "patch"}, true)
	writeRecord(t, dir, "rust-fix.md", "Fix the backend.", map[string]string{"rust@app": "patch"}, true)
	plan, err := Build(context.Background(), aliasedDeps(t, dir, config.VersionScopePackage))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	tags := map[string]string{}
	for _, release := range plan.Releases {
		tags[release.Name] = release.Tag
	}
	if tags["npm@app"] != "npm-app-v1.0.1" {
		t.Fatalf("npm tag = %q", tags["npm@app"])
	}
	if tags["rust@app"] != "rust-app-v1.0.1" {
		t.Fatalf("rust tag = %q", tags["rust@app"])
	}
}
