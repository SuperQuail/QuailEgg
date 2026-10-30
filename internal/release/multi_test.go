package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"time"
)

func newWorkspaceRepo(t *testing.T, versions map[string]string) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	root := "{\n  \"name\": \"root\",\n  \"private\": true,\n  \"workspaces\": [\"packages/*\"]\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, version := range versions {
		dirPath := filepath.Join(dir, "packages", name)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "{\n  \"name\": \"" + name + "\",\n  \"version\": \"" + version + "\"\n}\n"
		if err := os.WriteFile(filepath.Join(dirPath, "package.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial workspace")
	return dir
}

func writeRecord(t *testing.T, dir, name, body string, packages map[string]string, release bool) {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\ntype: feat\n")
	if !release {
		b.WriteString("release: false\n")
	}
	b.WriteString("packages:\n")
	for pkg, bump := range packages {
		b.WriteString("  " + pkg + ": " + bump + "\n")
	}
	b.WriteString("---\n\n" + body + "\n")
	if err := os.WriteFile(filepath.Join(dir, ".changes", name), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "test: "+name)
}

func workspaceDeps(t *testing.T, dir, scope string) *Deps {
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
		"core": {Path: "packages/core", Resolver: "npm", Publish: &publish},
		"cli":  {Path: "packages/cli", Resolver: "npm", Publish: &publish},
	}
	cfg.Release.VersionScope = scope
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): bump versions"
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

func TestIndependentPlanAndRelease(t *testing.T) {
	dir := newWorkspaceRepo(t, map[string]string{"core": "1.0.0", "cli": "1.0.0"})
	writeRecord(t, dir, "core-fix.md", "Fix a core bug.", map[string]string{"core": "patch"}, true)
	writeRecord(t, dir, "cli-feature.md", "Add a cli feature.", map[string]string{"cli": "minor"}, true)
	d := workspaceDeps(t, dir, config.VersionScopePackage)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q", plan.Phase)
	}
	if len(plan.Releases) != 2 {
		t.Fatalf("releases = %+v", plan.Releases)
	}
	if plan.Version.Next != "" || plan.Tag != "" {
		t.Fatalf("independent plan must not carry a single version: %+v tag %q", plan.Version, plan.Tag)
	}
	byName := map[string]ReleasePlan{}
	for _, release := range plan.Releases {
		byName[release.Name] = release
	}
	if got := byName["core"]; got.Version != "1.0.1" || got.Tag != "core-v1.0.1" {
		t.Fatalf("core release = %+v", got)
	}
	if got := byName["cli"]; got.Version != "1.1.0" || got.Tag != "cli-v1.1.0" {
		t.Fatalf("cli release = %+v", got)
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for name, want := range map[string]string{"core": "1.0.1", "cli": "1.1.0"} {
		data, err := os.ReadFile(filepath.Join(dir, "packages", name, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s manifest not bumped to %s:\n%s", name, want, data)
		}
	}
	changelogData, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"<!-- qegg:release core@1.0.1 -->", "<!-- qegg:release cli@1.1.0 -->"} {
		if !strings.Contains(string(changelogData), marker) {
			t.Fatalf("changelog is missing %s:\n%s", marker, changelogData)
		}
	}
	for _, tag := range []string{"core-v1.0.1", "cli-v1.1.0"} {
		if !hasTag(t, dir, tag) {
			t.Fatalf("tag %s missing", tag)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".changes", "core-fix.md")); !os.IsNotExist(err) {
		t.Fatal("consumed record was not removed")
	}
	marker, err := state.LoadMarker(dir)
	if err != nil || marker == nil || len(marker.Releases) != 2 {
		t.Fatalf("marker = %+v, err = %v", marker, err)
	}

	again, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after release: %v", err)
	}
	if again.Phase != PhaseNone {
		t.Fatalf("phase after release = %q, want none", again.Phase)
	}
}

func TestSharedVersionPlanAndRelease(t *testing.T) {
	dir := newWorkspaceRepo(t, map[string]string{"core": "2.0.0", "cli": "2.0.0"})
	writeRecord(t, dir, "core-fix.md", "Fix a core bug.", map[string]string{"core": "patch"}, true)
	d := workspaceDeps(t, dir, config.VersionScopeShared)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Releases) != 1 || plan.Version.Next != "2.0.1" || plan.Tag != "v2.0.1" {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Packages) != 2 {
		t.Fatalf("packages = %+v", plan.Packages)
	}
	for _, pkg := range plan.Packages {
		if pkg.Next != "2.0.1" {
			t.Fatalf("package %s next = %q", pkg.Name, pkg.Next)
		}
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{"core", "cli"} {
		data, err := os.ReadFile(filepath.Join(dir, "packages", name, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "2.0.1") {
			t.Fatalf("%s manifest not bumped:\n%s", name, data)
		}
	}
	changelogData, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changelogData), "<!-- qegg:release 2.0.1 -->") {
		t.Fatalf("changelog = %s", changelogData)
	}
	if !hasTag(t, dir, "v2.0.1") {
		t.Fatal("shared tag missing")
	}
}

func TestSharedVersionMismatchFails(t *testing.T) {
	dir := newWorkspaceRepo(t, map[string]string{"core": "2.0.0", "cli": "2.0.1"})
	writeRecord(t, dir, "core-fix.md", "Fix a core bug.", map[string]string{"core": "patch"}, true)
	_, err := Build(context.Background(), workspaceDeps(t, dir, config.VersionScopeShared))
	if err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("Build error = %v, want a shared version error", err)
	}
}

func TestReleaseFalseRecordStaysForOtherPackage(t *testing.T) {
	dir := newWorkspaceRepo(t, map[string]string{"core": "1.0.0", "cli": "1.0.0"})
	writeRecord(t, dir, "core-fix.md", "Fix a core bug.", map[string]string{"core": "patch"}, true)
	writeRecord(t, dir, "cli-note.md", "Document the cli.", map[string]string{"cli": "patch"}, false)
	d := workspaceDeps(t, dir, config.VersionScopePackage)
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Releases) != 1 || plan.Releases[0].Name != "core" {
		t.Fatalf("releases = %+v", plan.Releases)
	}
	if err := Run(context.Background(), d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".changes", "cli-note.md")); err != nil {
		t.Fatal("the record of a non-releasing package must stay pending")
	}
	data, err := os.ReadFile(filepath.Join(dir, "packages", "cli", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "1.0.0") {
		t.Fatalf("cli must stay at 1.0.0:\n%s", data)
	}
}

func TestTagCollisionFails(t *testing.T) {
	dir := newWorkspaceRepo(t, map[string]string{"core": "1.0.0", "cli": "1.0.0"})
	writeRecord(t, dir, "core-fix.md", "Fix a core bug.", map[string]string{"core": "patch"}, true)
	writeRecord(t, dir, "cli-fix.md", "Fix a cli bug.", map[string]string{"cli": "patch"}, true)
	d := workspaceDeps(t, dir, config.VersionScopePackage)
	d.Cfg.Git.TagFormat = "v{{version}}"
	_, err := Build(context.Background(), d)
	if err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("Build error = %v, want a tag collision", err)
	}
}

func hasTag(t *testing.T, dir, name string) bool {
	t.Helper()
	out, err := execx.OSRunner{}.Run(context.Background(), dir, "git", "tag", "--list", name)
	if err != nil {
		t.Fatalf("git tag: %v", err)
	}
	return strings.TrimSpace(out.Stdout) == name
}

func newPythonWorkspaceRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	rootManifest := "[tool.uv.workspace]\nmembers = [\"packages/*\", \"app\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(rootManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"packages/core/pyproject.toml": "[project]\nname = \"core\"\nversion = \"1.0.0\"\n",
		"app/pyproject.toml":           "[project]\nname = \"app\"\nversion = \"1.0.0\"\ndependencies = [\n  \"core==1.0.0\",\n]\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := "---\ntype: fix\npackages:\n  core: patch\n---\n\nFix a core bug.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "core-fix.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial workspace")
	return dir
}

func TestSharedPythonWorkspaceSyncsPins(t *testing.T) {
	dir := newPythonWorkspaceRepo(t)
	d := workspacePythonDeps(t, dir, config.VersionScopeShared)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "1.0.1" || len(plan.Releases) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "app", "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"core==1.0.1\"") {
		t.Fatalf("app pin was not synced:\n%s", data)
	}
}

func workspacePythonDeps(t *testing.T, dir, scope string) *Deps {
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
		"core": {Path: "packages/core", Resolver: "python", Publish: &publish},
		"app":  {Path: "app", Resolver: "python", Publish: &publish},
	}
	cfg.Release.VersionScope = scope
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): bump versions"
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

func TestIndependentPythonWorkspaceSyncsPins(t *testing.T) {
	dir := newPythonWorkspaceRepo(t)
	d := workspacePythonDeps(t, dir, config.VersionScopePackage)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Releases) != 1 || plan.Releases[0].Name != "core" {
		t.Fatalf("plan releases = %+v", plan.Releases)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "app", "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"core==1.0.1\"") {
		t.Fatalf("app pin was not synced in package scope:\n%s", data)
	}
}

func newRustWorkspaceRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	files := map[string]string{
		"Cargo.toml": `[workspace]
members = ["crates/core", "crates/cli"]

[workspace.package]
version = "1.0.0"
edition = "2021"

[workspace.dependencies]
rust-core = { path = "crates/core" }
`,
		"crates/core/Cargo.toml": `[package]
name = "rust-core"
version.workspace = true
edition.workspace = true
`,
		"crates/cli/Cargo.toml": `[package]
name = "rust-cli"
version.workspace = true
edition.workspace = true

[dependencies]
rust-core = { path = "../core" }
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
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := "---\ntype: fix\npackages:\n  rust-core: patch\n---\n\nFix a core bug.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "core-fix.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial workspace")
	return dir
}

func rustWorkspaceDeps(t *testing.T, dir, scope string) *Deps {
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
		"rust-core": {Path: "crates/core", Resolver: "rust", Publish: &publish},
		"rust-cli":  {Path: "crates/cli", Resolver: "rust", Publish: &publish},
	}
	cfg.Release.VersionScope = scope
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): bump versions"
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

func TestSharedRustWorkspaceSyncsWorkspaceVersion(t *testing.T) {
	dir := newRustWorkspaceRepo(t)
	d := rustWorkspaceDeps(t, dir, config.VersionScopeShared)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "1.0.1" || len(plan.Releases) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	names := make([]string, 0, len(plan.Packages))
	for _, pkg := range plan.Packages {
		names = append(names, pkg.Name)
	}
	if len(names) != 2 || names[0] != "rust-core" || names[1] != "rust-cli" {
		t.Fatalf("publish order = %v, want the dependency first", names)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rootManifest := readWorkspaceFile(t, dir, "Cargo.toml")
	for _, want := range []string{
		"version = \"1.0.1\"",
		`rust-core = { path = "crates/core", version = "1.0.1" }`,
	} {
		if !strings.Contains(rootManifest, want) {
			t.Fatalf("root manifest is missing %q:\n%s", want, rootManifest)
		}
	}
	cliManifest := readWorkspaceFile(t, dir, "crates/cli/Cargo.toml")
	if !strings.Contains(cliManifest, `rust-core = { path = "../core", version = "1.0.1" }`) {
		t.Fatalf("cli manifest does not pin the new version:\n%s", cliManifest)
	}
}

func TestRustWorkspaceRejectsPerPackageInheritedVersions(t *testing.T) {
	dir := newRustWorkspaceRepo(t)
	d := rustWorkspaceDeps(t, dir, config.VersionScopePackage)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	warned := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "version_scope") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("plan warnings = %v, want a version_scope warning", plan.Warnings)
	}
	if err := Run(ctx, d, plan); err == nil || !strings.Contains(err.Error(), "version_scope") {
		t.Fatalf("err = %v, want a version_scope error", err)
	}
}

func readWorkspaceFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOrderTargetsHonoursConfiguredDependencies(t *testing.T) {
	targets := []target{
		{name: "cli", pkg: ecosystem.Package{Name: "cli"}},
		{name: "core", pkg: ecosystem.Package{Name: "core"}},
	}
	ordered := orderTargets(targets, map[string]config.Package{"cli": {DependsOn: []string{"core"}}})
	if len(ordered) != 2 || ordered[0].name != "core" || ordered[1].name != "cli" {
		t.Fatalf("ordered = %v, want the configured dependency first", ordered)
	}
}

func TestConfiguredPackageOutsideDetection(t *testing.T) {
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "{\n  \"name\": \"ui-pkg\",\n  \"version\": \"0.1.0\",\n  \"private\": true\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "ui", "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := "---\ntype: fix\npackages:\n  ui-pkg: patch\n---\n\nFix a frontend bug.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "ui-fix.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial")

	repo, err := git.Open(context.Background(), dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	publish := false
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"ui-pkg": {Path: "ui", Resolver: "npm", Publish: &publish},
	}
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): bump versions"
	cfg.Verify.Commands = nil
	d := &Deps{
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
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion || len(plan.Packages) != 1 || plan.Packages[0].Current != "0.1.0" {
		t.Fatalf("plan = %+v", plan)
	}
	if err := Run(context.Background(), d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ui", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"version\": \"0.1.1\"") {
		t.Fatalf("nested package was not bumped:\n%s", data)
	}
}
