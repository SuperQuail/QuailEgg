package release

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// newSharedTagRepo builds the smallest repository that mixes a tag-versioned
// package (Go at the root) with a manifest-versioned one (npm in frontend/)
// under a shared version scope.
func newSharedTagRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	writeSharedFile(t, dir, "go.mod", "module example.com/demo\n\ngo 1.21\n")
	writeSharedFile(t, dir, "demo.go", "package demo\n\nconst Name = \"demo\"\n")
	writeSharedFile(t, dir, "frontend/package.json", "{\n  \"name\": \"frontend\",\n  \"version\": \"0.0.0\",\n  \"private\": true\n}\n")
	writeSharedFile(t, dir, ".changes/README.md", "# Change records\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial")
	// pr mode releases from the default branch, so the fixture needs its name.
	gitRun(t, dir, "branch", "-M", "main")
	return dir
}

// writeSharedFile writes one repository file, creating its directory.
func writeSharedFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sharedFrontendManifest renders the npm manifest the fixture starts from.
func sharedFrontendManifest(version string) string {
	return "{\n  \"name\": \"frontend\",\n  \"version\": \"" + version + "\",\n  \"private\": true\n}\n"
}

// sharedTagDeps configures the two packages the shared release has to keep in
// step: Go at the root and npm in frontend/.
func sharedTagDeps(t *testing.T, dir string) *Deps {
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
		"go@demo":      {Name: "example.com/demo", Path: ".", Resolver: config.ResolverGo, Publish: &publish},
		"npm@frontend": {Name: "frontend", Path: "frontend", Resolver: config.ResolverNPM, Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.Mode = config.ModePR
	cfg.Release.GitHubRelease = false
	cfg.Verify.Commands = nil
	cfg.Verify.Timeout = "5m"
	return &Deps{
		Root:    dir,
		Cfg:     cfg,
		Git:     repo,
		Run:     execx.OSRunner{},
		UI:      ui.Discard(),
		Now:     time.Now,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
		Options: Options{SkipVerify: true, NoPublish: true, NoPush: true, NoGitHubRelease: true},
	}
}

// writeSharedRecord writes a change record that bumps both packages and commits
// it, so the release run starts from a clean working tree.
func writeSharedRecord(t *testing.T, dir, name, bump, body string) {
	t.Helper()
	record := "---\ntype: feat\npackages:\n  go@demo: " + bump + "\n  npm@frontend: " + bump + "\n---\n\n" + body + "\n"
	writeSharedFile(t, dir, filepath.ToSlash(filepath.Join(".changes", name)), record)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "feat: "+strings.TrimSuffix(name, ".md"))
}

// saveSharedMarker writes the committed release intent the publish phase reads.
func saveSharedMarker(t *testing.T, dir, version string, releases []state.MarkerRelease) {
	t.Helper()
	marker := &state.Marker{
		SchemaVersion: 1,
		Version:       version,
		Mode:          config.ModePR,
		Releases:      releases,
	}
	if err := state.SaveMarker(dir, marker); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
}

// sharedPackage finds one planned package by identity.
func sharedPackage(t *testing.T, plan *Plan, name string) PackagePlan {
	t.Helper()
	for _, pkg := range plan.Packages {
		if pkg.Name == name {
			return pkg
		}
	}
	t.Fatalf("plan does not list package %q: %+v", name, plan.Packages)
	return PackagePlan{}
}

// sharedCurrentVersion reads one package's version the way the release does.
func sharedCurrentVersion(t *testing.T, d *Deps, pkg ecosystem.Package) string {
	t.Helper()
	version, err := registry.CurrentVersion(context.Background(), d.Run, d.Root, pkg, d.Cfg.Git.TagPrefix)
	if err != nil {
		t.Fatalf("CurrentVersion(%s): %v", pkg.Name, err)
	}
	return version
}

// TestSharedReleasePublishesTagVersionedPackage is the regression this fix is
// about: the version phase bumps the npm manifest while Go stays on its
// previous tag, and the publish phase has to read the marker's pending version
// instead of comparing the two current versions.
func TestSharedReleasePublishesTagVersionedPackage(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	ctx := context.Background()

	writeSharedRecord(t, dir, "add-feature.md", "minor", "Add a feature.")

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want %q", plan.Phase, PhaseVersion)
	}
	if plan.Version.Next != "0.1.0" || plan.Tag != "v0.1.0" {
		t.Fatalf("version = %+v tag = %q, want 0.1.0 / v0.1.0", plan.Version, plan.Tag)
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run version phase: %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(dir, "frontend", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "\"version\": \"0.1.0\"") {
		t.Fatalf("frontend manifest was not bumped to 0.1.0:\n%s", manifest)
	}
	marker, err := state.LoadMarker(dir)
	if err != nil {
		t.Fatalf("LoadMarker: %v", err)
	}
	if marker == nil {
		t.Fatal("the version phase did not write the marker")
	}
	if marker.Version != "0.1.0" {
		t.Fatalf("marker version = %q, want %q", marker.Version, "0.1.0")
	}
	if hasTag(t, dir, "v0.1.0") {
		t.Fatal("the version phase must not create the tag")
	}
	// A Go module has no manifest version: it reports the previous tag until the
	// publish phase creates the new one, so the two packages legitimately differ
	// between the phases.
	goPkg := ecosystem.Package{Name: "example.com/demo", Path: ".", Resolver: config.ResolverGo}
	if got := sharedCurrentVersion(t, d, goPkg); got != "0.0.0" {
		t.Fatalf("go version after the version phase = %q, want %q", got, "0.0.0")
	}

	// The publish phase rebuilds from the merged tree: the tag does not exist
	// yet, so the Go package still reports 0.0.0 while npm is at 0.1.0.
	publish, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after the version phase: %v", err)
	}
	if publish.Phase != PhasePublish {
		t.Fatalf("phase = %q, want %q", publish.Phase, PhasePublish)
	}
	if publish.Version.Next != "0.1.0" || publish.Tag != "v0.1.0" {
		t.Fatalf("version = %+v tag = %q, want 0.1.0 / v0.1.0", publish.Version, publish.Tag)
	}
	if publish.HasChanges {
		t.Fatal("the publish phase must not consume change records again")
	}
	// The publish plan lists the marker's record of the plan, so both packages
	// carry the version they had when the version phase started.
	for _, name := range []string{"go@demo", "npm@frontend"} {
		if got := sharedPackage(t, publish, name); got.Current != "0.0.0" || got.Next != "0.1.0" {
			t.Fatalf("%s = %+v, want current 0.0.0 next 0.1.0", name, got)
		}
	}

	if err := Run(ctx, d, publish); err != nil {
		t.Fatalf("Run publish phase: %v", err)
	}
	if !hasTag(t, dir, "v0.1.0") {
		t.Fatal("the publish phase did not create v0.1.0")
	}

	// The marker stays behind after the release, but its tag exists now, so it
	// must not block the next plan.
	writeSharedRecord(t, dir, "add-second.md", "patch", "Fix a second thing.")
	next, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build for the second release: %v", err)
	}
	if next.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want %q", next.Phase, PhaseVersion)
	}
	if next.Version.Next != "0.1.1" {
		t.Fatalf("next = %q, want %q", next.Version.Next, "0.1.1")
	}
}

// A marker that plans a version below the tag Go already carries cannot be
// published: the release it names is older than the checked out module.
func TestSharedReleaseRejectsTagAheadOfPlan(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: bump frontend")
	tagGo(t, dir, "v0.2.0")
	saveSharedMarker(t, dir, "0.1.0", []state.MarkerRelease{{
		Version:  "0.1.0",
		Tag:      "v0.1.0",
		Packages: []string{"go@demo", "npm@frontend"},
	}})

	_, err := Build(context.Background(), d)
	if err == nil {
		t.Fatal("Build succeeded, want a shared version error")
	}
	// The tag ahead of the plan has to be reported against the pending release,
	// not as a plain mismatch: the message names the package version and the
	// version the marker is preparing, so degrading the rule to a strict equality
	// check would change what the user is told.
	for _, want := range []string{"pending release", "0.2.0", "0.1.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Build error = %v, want it to mention %q", err, want)
		}
	}
}

// Once the tag exists the marker is history, and the strict check applies
// again: both packages sit on the released version, so the plan is a no-op.
func TestSharedReleaseKeepsStrictCheckAfterPublish(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: bump frontend")
	tagGo(t, dir, "v0.1.0")
	saveSharedMarker(t, dir, "0.1.0", nil)

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseNone {
		t.Fatalf("phase = %q, want %q", plan.Phase, PhaseNone)
	}
}

// A prerelease is pending in the same way: the manifest already carries it and
// the Go module has no tag yet, so the marker supplies the reference version.
func TestSharedReleaseAcceptsPendingPrerelease(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0-alpha.1"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: bump frontend")
	saveSharedMarker(t, dir, "0.1.0-alpha.1", []state.MarkerRelease{{
		Version:    "0.1.0-alpha.1",
		Tag:        "v0.1.0-alpha.1",
		Packages:   []string{"go@demo", "npm@frontend"},
		Prerelease: "alpha",
	}})

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want %q", plan.Phase, PhasePublish)
	}
	if plan.Version.Next != "0.1.0-alpha.1" {
		t.Fatalf("next = %q, want %q", plan.Version.Next, "0.1.0-alpha.1")
	}
	if plan.Tag != "v0.1.0-alpha.1" {
		t.Fatalf("tag = %q, want %q", plan.Tag, "v0.1.0-alpha.1")
	}
}

// newSharedPythonRepo builds the shared unit that mixes a PEP 440 manifest
// package (python in packages/core) with a semver one (npm in frontend), so the
// two ecosystems spell the same version differently in a tag.
func newSharedPythonRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	writeSharedFile(t, dir, "packages/core/pyproject.toml", "[project]\nname = \"core\"\nversion = \"0.1.0\"\n")
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0"))
	writeSharedFile(t, dir, ".changes/README.md", "# Change records\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: initial")
	// pr mode releases from the default branch, so the fixture needs its name.
	gitRun(t, dir, "branch", "-M", "main")
	return dir
}

// sharedPythonDeps registers the python and the npm package under one shared
// version.
func sharedPythonDeps(t *testing.T, dir string) *Deps {
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
		"npm@frontend": {Name: "frontend", Path: "frontend", Resolver: config.ResolverNPM, Publish: &publish},
		"python@core":  {Name: "core", Path: "packages/core", Resolver: config.ResolverPython, Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.Mode = config.ModePR
	cfg.Release.GitHubRelease = false
	cfg.Verify.Commands = nil
	cfg.Verify.Timeout = "5m"
	return &Deps{
		Root:    dir,
		Cfg:     cfg,
		Git:     repo,
		Run:     execx.OSRunner{},
		UI:      ui.Discard(),
		Now:     time.Now,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
		Options: Options{SkipVerify: true, NoPublish: true, NoPush: true, NoGitHubRelease: true},
	}
}

// The prerelease tag exists and the channel lock is gone, so the next release is
// the stable one. The Go module reports the prerelease tag as its version while
// the npm manifest still carries the prerelease; graduation has to turn both
// into 0.1.0 without a pending release to read from.
func TestSharedReleaseGraduatesPrereleaseWithTagVersionedPackage(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0-alpha.1"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: bump frontend to the prerelease")
	tagGo(t, dir, "v0.1.0-alpha.1")
	saveSharedMarker(t, dir, "0.1.0-alpha.1", []state.MarkerRelease{{
		Version:    "0.1.0-alpha.1",
		Tag:        "v0.1.0-alpha.1",
		Packages:   []string{"go@demo", "npm@frontend"},
		Prerelease: "alpha",
	}})
	// No pre-release.lock: the channel was left, so the release graduates.

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want %q", plan.Phase, PhaseVersion)
	}
	if plan.Version.Next != "0.1.0" {
		t.Fatalf("next = %q, want %q", plan.Version.Next, "0.1.0")
	}
	if !plan.Graduating {
		t.Fatalf("graduating = false, want the prerelease to graduate: %+v", plan.Version)
	}
	if plan.Tag != "v0.1.0" {
		t.Fatalf("tag = %q, want %q", plan.Tag, "v0.1.0")
	}
	if plan.Channel != "" {
		t.Fatalf("channel = %q, want the stable release to carry none", plan.Channel)
	}
}

// A marker written before releases were recorded carries one version for the
// whole repository. The manifest already holds it while the Go module has no tag
// yet, so the marker's Version field alone has to make this a publish plan.
func TestSharedReleaseUsesLegacyMarkerPending(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: bump frontend")
	if hasTag(t, dir, "v0.1.0") {
		t.Fatal("the fixture must not carry the pending tag yet")
	}
	// Releases stays nil: this is the legacy shape of the marker.
	saveSharedMarker(t, dir, "0.1.0", nil)

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want %q", plan.Phase, PhasePublish)
	}
	if plan.Version.Next != "0.1.0" {
		t.Fatalf("next = %q, want %q", plan.Version.Next, "0.1.0")
	}
	if plan.Tag != "v0.1.0" {
		t.Fatalf("tag = %q, want %q", plan.Tag, "v0.1.0")
	}
}

// A pending release that does not cover a registered package can never write
// that package's version, so the plan has to name it instead of blaming it for
// version drift.
func TestSharedReleaseReportsUncoveredPackage(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	publish := false
	d.Cfg.Packages["npm@admin"] = config.Package{Name: "admin", Path: "admin", Resolver: config.ResolverNPM, Publish: &publish}
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0"))
	writeSharedFile(t, dir, "admin/package.json", "{\n  \"name\": \"admin\",\n  \"version\": \"0.1.0\",\n  \"private\": true\n}\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: add the admin package")
	saveSharedMarker(t, dir, "0.1.0", []state.MarkerRelease{{
		Version:  "0.1.0",
		Tag:      "v0.1.0",
		Packages: []string{"go@demo", "npm@frontend"},
	}})

	_, err := Build(context.Background(), d)
	if err == nil {
		t.Fatal("Build succeeded, want the uncovered package to be reported")
	}
	for _, want := range []string{"does not cover", "npm@admin", "0.1.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Build error = %v, want it to mention %q", err, want)
		}
	}
}

// One tag carries one version for the whole shared unit, so the ecosystems have
// to spell a prerelease the same way. PEP 440 normalises 0.2.0-alpha.1 to
// 0.2.0a1 while npm keeps the semver spelling, and the plan must refuse that
// instead of creating a tag only one ecosystem understands.
func TestSharedReleaseRejectsTagSpellingForPrerelease(t *testing.T) {
	dir := newSharedPythonRepo(t)
	d := sharedPythonDeps(t, dir)
	writeRecord(t, dir, "add-feature.md", "Add a feature.", map[string]string{
		"npm@frontend": "minor",
		"python@core":  "minor",
	}, true)
	writeLock(t, dir, "alpha", "")

	_, err := Build(context.Background(), d)
	if err == nil {
		t.Fatal("Build succeeded, want a tag spelling error")
	}
	for _, want := range []string{"python@core", "0.2.0a1", "npm@frontend", "0.2.0-alpha.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Build error = %v, want it to mention %q", err, want)
		}
	}
}

// The version phase writes the planned version into the version files, so the
// publish phase has to compare them against that version. Comparing against the
// checked out version of a tag-versioned package would report every version file
// as drift right after the version phase updated it.
func TestPlanPublishPhaseVersionFileWarning(t *testing.T) {
	dir := newSharedTagRepo(t)
	d := sharedTagDeps(t, dir)
	d.Cfg.Version.File = []config.VersionFile{{Path: "version.json", Format: "json", Key: "version"}}
	writeSharedFile(t, dir, "frontend/package.json", sharedFrontendManifest("0.1.0"))
	writeSharedFile(t, dir, "version.json", "{\n  \"version\": \"0.1.0\"\n}\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: bump frontend and the version file")
	saveSharedMarker(t, dir, "0.1.0", []state.MarkerRelease{{
		Version:  "0.1.0",
		Tag:      "v0.1.0",
		Packages: []string{"go@demo", "npm@frontend"},
	}})

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want %q", plan.Phase, PhasePublish)
	}
	if plan.Version.Next != "0.1.0" {
		t.Fatalf("next = %q, want %q", plan.Version.Next, "0.1.0")
	}
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "version.json") {
			t.Fatalf("the version file holds the planned version %q, got warning %q", plan.Version.Next, warning)
		}
	}

	// The warning is still live: a version file that lags behind the plan is
	// reported, so the assertion above is not vacuous.
	writeSharedFile(t, dir, "version.json", "{\n  \"version\": \"0.0.9\"\n}\n")
	drifted, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build after the version file drifted: %v", err)
	}
	found := false
	for _, warning := range drifted.Warnings {
		if strings.Contains(warning, "version.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a version file behind the plan must warn, got %v", drifted.Warnings)
	}
}
