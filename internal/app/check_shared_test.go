package app

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// newSharedCheckRepo writes the fixture files and commits them, so the tag
// lookups qegg check performs see a normal repository.
func newSharedCheckRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "check@example.com")
	runGit(t, dir, "config", "user.name", "check")
	for rel, body := range files {
		writeRepoFile(t, dir, rel, body)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "chore: initial")
	return dir
}

// sharedCheckApp builds the App over a real repository: the pending-release
// lookup asks git whether the marker's tags already exist.
func sharedCheckApp(t *testing.T, dir string, cfg *config.Config) *App {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	repo, err := git.Open(context.Background(), dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	return &App{Root: dir, Cfg: cfg, Run: execx.OSRunner{}, Git: repo}
}

// sharedCheckConfig registers the Go module at the root and the npm package in
// frontend/ under one shared version.
func sharedCheckConfig() *config.Config {
	cfg := config.Default()
	publish := false
	cfg.Packages = map[string]config.Package{
		"go@demo":      {Name: "example.com/demo", Path: ".", Resolver: config.ResolverGo, Publish: &publish},
		"npm@frontend": {Name: "frontend", Path: "frontend", Resolver: config.ResolverNPM, Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.GitHubRelease = false
	cfg.Verify.Commands = nil
	return cfg
}

// sharedCheckFiles is the two-package fixture with the npm manifest at version.
func sharedCheckFiles(version string) map[string]string {
	return map[string]string{
		"go.mod":                "module example.com/demo\n\ngo 1.21\n",
		"demo.go":               "package demo\n\nconst Name = \"demo\"\n",
		"frontend/package.json": "{\n  \"name\": \"frontend\",\n  \"version\": \"" + version + "\",\n  \"private\": true\n}\n",
	}
}

// saveSharedCheckMarker writes the committed release intent qegg check reads.
func saveSharedCheckMarker(t *testing.T, dir, version string, releases []state.MarkerRelease) {
	t.Helper()
	marker := &state.Marker{SchemaVersion: 1, Version: version, Mode: config.ModePR, Releases: releases}
	if err := state.SaveMarker(dir, marker); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
}

// Without a pending release the two packages have to be in step, and a Go
// module without a tag reports 0.0.0.
func TestCheckReportsSharedDriftWithoutMarker(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.1.0"))
	a := sharedCheckApp(t, dir, sharedCheckConfig())

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var mismatch *Problem
	for i := range report.Problems {
		if report.Problems[i].Code == "shared-version-mismatch" {
			mismatch = &report.Problems[i]
			break
		}
	}
	if mismatch == nil {
		t.Fatalf("missing shared-version-mismatch problem: %+v", report.Problems)
	}
	// The message has to name both sides, or the reader cannot see which package
	// is behind and which one defines the version.
	for _, want := range []string{"npm@frontend", "0.1.0", "go@demo", "0.0.0"} {
		if !strings.Contains(mismatch.Message, want) {
			t.Fatalf("problem = %+v, want it to name %q", mismatch, want)
		}
	}
	if report.OK {
		t.Fatalf("shared drift must fail the check: %+v", report.Problems)
	}
}

// The version phase already wrote the pending version into the npm manifest
// while Go has no tag yet, so the marker supplies the reference version.
func TestCheckAcceptsPendingSharedRelease(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.1.0-alpha.1"))
	saveSharedCheckMarker(t, dir, "0.1.0-alpha.1", []state.MarkerRelease{{
		Version:  "0.1.0-alpha.1",
		Tag:      "v0.1.0-alpha.1",
		Packages: []string{"go@demo", "npm@frontend"},
	}})
	a := sharedCheckApp(t, dir, sharedCheckConfig())

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, code := range []string{"shared-version-mismatch", "shared-version-pending"} {
		if hasProblem(report, code) {
			t.Fatalf("a pending release still reports %s: %+v", code, report.Problems)
		}
	}
	// The report names the version being prepared, so the caller can tell a
	// manifest that lags behind a release from one that is simply wrong.
	if report.PendingVersion != "0.1.0-alpha.1" {
		t.Fatalf("PendingVersion = %q, want %q", report.PendingVersion, "0.1.0-alpha.1")
	}
	// Nothing is wrong with a repository whose packages sit where the pending
	// release put them, so the check has to pass.
	if !report.OK {
		t.Fatalf("a pending release in step must pass the check: %+v", report.Problems)
	}
}

// A tag-versioned package ahead of the release being prepared cannot join it:
// the tag it would create already exists. The problem has to name both versions
// and point at the pending release, not at manifests the version phase already
// wrote.
func TestCheckReportsSharedVersionPending(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.1.0"))
	runGit(t, dir, "tag", "v0.2.0")
	saveSharedCheckMarker(t, dir, "0.1.0", []state.MarkerRelease{{
		Version:  "0.1.0",
		Tag:      "v0.1.0",
		Packages: []string{"go@demo", "npm@frontend"},
	}})
	a := sharedCheckApp(t, dir, sharedCheckConfig())

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var pending *Problem
	for i := range report.Problems {
		if report.Problems[i].Code == "shared-version-pending" {
			pending = &report.Problems[i]
			break
		}
	}
	if pending == nil {
		t.Fatalf("missing shared-version-pending problem: %+v", report.Problems)
	}
	// Both versions have to appear, or the reader cannot see which one the
	// release is preparing.
	for _, want := range []string{"0.2.0", "0.1.0"} {
		if !strings.Contains(pending.Message, want) {
			t.Fatalf("problem = %+v, want it to name %q", pending, want)
		}
	}
	if want := i18n.T("check.fix.shared_version_pending"); pending.Fix != want {
		t.Fatalf("fix = %q, want %q", pending.Fix, want)
	}
}

// One tag carries one version, so only a tag-versioned package in a
// subdirectory is impossible; the root module can share the repository version.
// The plan accepts such a repository, so check must not invent a shared-version
// problem for it.
func TestCheckAcceptsRootTagVersionedShared(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.0.0"))
	a := sharedCheckApp(t, dir, sharedCheckConfig())

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var shared []string
	for _, problem := range report.Problems {
		if strings.HasPrefix(problem.Code, "shared-") {
			shared = append(shared, problem.Code+": "+problem.Message)
		}
	}
	if len(shared) > 0 {
		t.Fatalf("a root Go module shares the repository version, got %v", shared)
	}
	if hasProblem(report, "package-tag-versioned") {
		t.Fatalf("shared scope must not report package-tag-versioned: %+v", report.Problems)
	}
}

// A version file carries the release version, so it is compared against the
// pending one while a tag-versioned package still reports its previous tag.
func TestCheckUsesPendingVersionForVersionFile(t *testing.T) {
	dir := newSharedCheckRepo(t, map[string]string{
		"go.mod":       "module example.com/demo\n\ngo 1.21\n",
		"demo.go":      "package demo\n\nconst Name = \"demo\"\n",
		"version.json": "{\n  \"version\": \"1.2.0\"\n}\n",
	})
	runGit(t, dir, "tag", "v1.1.0")
	publish := false
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"go@demo": {Name: "example.com/demo", Path: ".", Resolver: config.ResolverGo, Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.GitHubRelease = false
	cfg.Verify.Commands = nil
	cfg.Version.File = []config.VersionFile{{Path: "version.json", Format: "json", Key: "version"}}
	saveSharedCheckMarker(t, dir, "1.2.0", []state.MarkerRelease{{
		Version:  "1.2.0",
		Tag:      "v1.2.0",
		Packages: []string{"go@demo"},
	}})
	a := sharedCheckApp(t, dir, cfg)

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if hasProblem(report, "version-file-drift") {
		t.Fatalf("the pending version must be the reference: %+v", report.Problems)
	}

	// Without the marker the checked out tag is the reference again, so the
	// same file is reported as drift.
	if err := state.RemoveMarker(dir); err != nil {
		t.Fatalf("RemoveMarker: %v", err)
	}
	report, err = a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "version-file-drift") {
		t.Fatalf("missing version-file-drift after the marker was removed: %+v", report.Problems)
	}
}

// A marker that cannot be read leaves the version files unverifiable: the
// reference version is unknown, not absent. Falling back to "every package must
// agree" would blame a shared repository for drift the running release explains,
// with a fix that suggests the scope it already uses.
func TestCheckReportsUnverifiedVersionFilesForInvalidMarker(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.1.0"))
	writeRepoFile(t, dir, ".quailegg/release.json", "{ this is not json\n")
	cfg := sharedCheckConfig()
	cfg.Version.File = []config.VersionFile{{Path: "frontend/package.json", Format: "json", Key: "version"}}
	a := sharedCheckApp(t, dir, cfg)

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "version-file-unverified") {
		t.Fatalf("missing version-file-unverified problem: %+v", report.Problems)
	}
	if hasProblem(report, "version-file-ambiguous") {
		t.Fatalf("an unreadable marker must not fall back to the ambiguous rule: %+v", report.Problems)
	}
}

// A marker that cannot be parsed is a release blocker: the pending version is
// what the shared rules and the version files are compared against, so check has
// to report the unreadable file instead of silently planning without it.
func TestCheckReportsInvalidMarker(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.1.0"))
	writeRepoFile(t, dir, ".quailegg/release.json", "{ this is not json\n")
	a := sharedCheckApp(t, dir, sharedCheckConfig())

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "marker-invalid") {
		t.Fatalf("missing marker-invalid problem: %+v", report.Problems)
	}
	if report.PendingVersion != "" {
		t.Fatalf("PendingVersion = %q, want none for an unreadable marker", report.PendingVersion)
	}
	// An unreadable marker leaves the pending version unknown, not absent, so
	// the shared rules have to stay out of it: comparing the packages would add
	// a drift problem whose fix is wrong while a release is still running.
	if hasProblem(report, "shared-version-mismatch") {
		t.Fatalf("an unreadable marker also reported shared-version-mismatch: %+v", report.Problems)
	}
}

// Package scope tags every package on its own, which a tag-versioned package
// cannot do: its version comes from the repository tag. The plan refuses that
// repository, so check has to report it before a release gets there.
func TestCheckReportsPackageTagVersioned(t *testing.T) {
	dir := newSharedCheckRepo(t, sharedCheckFiles("0.1.0"))
	cfg := sharedCheckConfig()
	cfg.Release.VersionScope = config.VersionScopePackage
	a := sharedCheckApp(t, dir, cfg)

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "package-tag-versioned") {
		t.Fatalf("missing package-tag-versioned problem: %+v", report.Problems)
	}
	// The problem has to fail the check and name the package the user has to
	// move or reconfigure, or the report says nothing a reader can act on.
	if report.OK {
		t.Fatalf("a package-scoped tag-versioned package must fail the check: %+v", report.Problems)
	}
	for _, problem := range report.Problems {
		if problem.Code != "package-tag-versioned" {
			continue
		}
		if !strings.Contains(problem.Message, "go@demo") {
			t.Fatalf("problem = %+v, want it to name go@demo", problem)
		}
	}
}

// One tag carries the version of every package in a shared unit, so the
// ecosystems have to spell that version the same way in a tag. Python writes a
// PEP 440 prerelease as 2.0.2a14 while npm keeps 2.0.2-a14, and the two
// spellings would produce two different tags for one release.
func TestCheckReportsSharedTagSpelling(t *testing.T) {
	dir := newSharedCheckRepo(t, map[string]string{
		"core/pyproject.toml":   "[project]\nname = \"core\"\nversion = \"2.0.2-a14\"\n",
		"frontend/package.json": "{\n  \"name\": \"frontend\",\n  \"version\": \"2.0.2-a14\",\n  \"private\": true\n}\n",
	})
	publish := false
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"npm@frontend": {Name: "frontend", Path: "frontend", Resolver: config.ResolverNPM, Publish: &publish},
		"python@core":  {Name: "core", Path: "core", Resolver: config.ResolverPython, Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.GitHubRelease = false
	cfg.Verify.Commands = nil
	a := sharedCheckApp(t, dir, cfg)

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var spelling *Problem
	for i := range report.Problems {
		if report.Problems[i].Code == "shared-tag-spelling" {
			spelling = &report.Problems[i]
			break
		}
	}
	if spelling == nil {
		t.Fatalf("missing shared-tag-spelling problem: %+v", report.Problems)
	}
	// Both spellings have to appear, or the reader cannot see which tags the
	// release would create.
	for _, want := range []string{"2.0.2a14", "2.0.2-a14"} {
		if !strings.Contains(spelling.Message, want) {
			t.Fatalf("problem = %+v, want it to name the tag spelling %q", spelling, want)
		}
	}
}

// A pending release that leaves a package out never writes its version, so the
// untouched package would be blamed for drift it did not cause. Report the
// missing coverage instead, and name the package the release does not include.
func TestCheckReportsUncoveredPendingRelease(t *testing.T) {
	dir := newSharedCheckRepo(t, map[string]string{
		"go.mod":                "module example.com/demo\n\ngo 1.21\n",
		"demo.go":               "package demo\n\nconst Name = \"demo\"\n",
		"frontend/package.json": "{\n  \"name\": \"frontend\",\n  \"version\": \"0.1.0\",\n  \"private\": true\n}\n",
		"admin/package.json":    "{\n  \"name\": \"admin\",\n  \"version\": \"0.1.0\",\n  \"private\": true\n}\n",
	})
	saveSharedCheckMarker(t, dir, "0.1.0", []state.MarkerRelease{{
		Version:  "0.1.0",
		Tag:      "v0.1.0",
		Packages: []string{"go@demo", "npm@frontend"},
	}})
	cfg := sharedCheckConfig()
	publish := false
	cfg.Packages["npm@admin"] = config.Package{Name: "admin", Path: "admin", Resolver: config.ResolverNPM, Publish: &publish}
	a := sharedCheckApp(t, dir, cfg)

	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "shared-version-uncovered") {
		t.Fatalf("missing shared-version-uncovered problem: %+v", report.Problems)
	}
	if report.PendingVersion != "0.1.0" {
		t.Fatalf("PendingVersion = %q, want %q", report.PendingVersion, "0.1.0")
	}
	for _, problem := range report.Problems {
		if problem.Code != "shared-version-uncovered" {
			continue
		}
		if !strings.Contains(problem.Message, "npm@admin") || !strings.Contains(problem.Message, "0.1.0") {
			t.Fatalf("problem = %+v, want it to name npm@admin and the pending version", problem)
		}
	}
}
