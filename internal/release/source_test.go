package release

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// buildCall is one command the release ran, with the directory it ran in.
type buildCall struct {
	dir   string
	name  string
	args  []string
	watch string
}

// recordingRunner records where the release builds from and keeps the content
// of one watched file as it was inside that directory.
type recordingRunner struct {
	execx.OSRunner
	calls   []buildCall
	watched string
	content map[string]string
}

func (r *recordingRunner) record(dir, name string, args []string) {
	call := buildCall{dir: dir, name: name, args: append([]string(nil), args...)}
	if r.watched != "" {
		call.watch = r.watched
		if data, err := os.ReadFile(filepath.Join(dir, r.watched)); err == nil {
			if r.content == nil {
				r.content = map[string]string{}
			}
			r.content[dir] = string(data)
		}
	}
	r.calls = append(r.calls, call)
}

func (r *recordingRunner) Run(ctx context.Context, dir, name string, args ...string) (execx.Result, error) {
	r.record(dir, name, args)
	return r.OSRunner.Run(ctx, dir, name, args...)
}

func (r *recordingRunner) RunEnv(ctx context.Context, dir string, env []string, name string, args ...string) (execx.Result, error) {
	r.record(dir, name, args)
	return r.OSRunner.RunEnv(ctx, dir, env, name, args...)
}

// sourceRepo prepares a Go module whose banner names the commit it was written
// in, so a build from the wrong tree is visible in the output.
func sourceRepo(t *testing.T, banner string) string {
	t.Helper()
	dir := newGoRepo(t)
	addGoBanner(t, dir, banner)
	return dir
}

// addGoBanner writes cmd/demo/main.go with the banner and commits nothing.
func addGoBanner(t *testing.T, dir, banner string) {
	t.Helper()
	main := "package main\n\nimport \"fmt\"\n\nconst banner = \"" + banner + "\"\n\nfunc main() { fmt.Println(banner) }\n"
	writeGoFile(t, filepath.Join(dir, "cmd", "demo", "main.go"), main)
}

// commitAll stages everything and commits, returning the new hash.
func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", message)
	return gitOutput(t, dir, "rev-parse", "HEAD")
}

// releaseMarker is the marker of a pending Go release that has no tag yet.
func releaseMarker() *state.Marker {
	return &state.Marker{
		SchemaVersion: 1,
		Version:       "0.1.0",
		Mode:          config.ModeCommit,
		Fingerprint:   "sha256:release-source",
		Packages: []state.MarkerPackage{{
			Name: "example.com/demo", Path: ".", Resolver: "go",
			Current: "0.0.0", Next: "0.1.0", Bump: "minor", Publish: false,
		}},
		Releases: []state.MarkerRelease{{Version: "0.1.0", Tag: "v0.1.0", Packages: []string{"example.com/demo"}}},
	}
}

// pendingRelease commits the marker as the release commit and returns its hash.
func pendingRelease(t *testing.T, dir string) string {
	t.Helper()
	if err := state.SaveMarker(dir, releaseMarker()); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	return commitAll(t, dir, "chore(release): v0.1.0")
}

func assetDeps(t *testing.T, dir string, run execx.Runner) *Deps {
	t.Helper()
	d := goDeps(t, dir)
	d.Cfg.Release.Assets.Targets = []string{hostTarget()}
	if run != nil {
		d.Run = run
	}
	return d
}

// TestPublishBuildsFromThePinnedSnapshot is the A/B case: the release commit A
// carries banner vA, the branch then moves to B with vB, and the resume must
// build, upload and tag the release that A described.
func TestPublishBuildsFromThePinnedSnapshot(t *testing.T) {
	dir := sourceRepo(t, "vA")
	release := pendingRelease(t, dir)
	// Move the branch on: the working tree is no longer the release snapshot.
	addGoBanner(t, dir, "vB")
	head := commitAll(t, dir, "feat: move on to vB")

	runner := &recordingRunner{watched: filepath.Join("cmd", "demo", "main.go")}
	d := assetDeps(t, dir, runner)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want publish", plan.Phase)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	points, err := d.Git.TagPointsAt(ctx, "v0.1.0", release)
	if err != nil || !points {
		t.Fatalf("v0.1.0 must point at the release commit %s: %v %v", release, points, err)
	}
	if points, err = d.Git.TagPointsAt(ctx, "v0.1.0", head); err != nil {
		t.Fatal(err)
	} else if points {
		t.Fatal("v0.1.0 must not point at the commit the branch moved to")
	}
	if after := gitOutput(t, dir, "rev-parse", "HEAD"); after != head {
		t.Fatalf("the publish phase must not move the branch: %s -> %s", head, after)
	}
	if last, err := state.LatestRun(dir); err != nil || last == nil || last.SourceCommit != release {
		t.Fatalf("the run record must pin the release commit: %+v, %v", last, err)
	}

	// The asset build ran in an isolated checkout of A, not in the working tree.
	if len(runner.calls) == 0 {
		t.Fatal("no build command ran")
	}
	for _, call := range runner.calls {
		if call.name != "go" {
			// Only the build commands matter here; the executor's git calls
			// rightfully run in the working tree.
			continue
		}
		if filepath.Clean(call.dir) == filepath.Clean(dir) {
			t.Fatalf("%s %v ran in the moved working tree", call.name, call.args)
		}
		if call.watch != "" && !strings.Contains(runner.content[call.dir], "vA") {
			t.Fatalf("the build read %q from %s, want the release snapshot content", runner.content[call.dir], call.dir)
		}
	}
	assetsDir := filepath.Join(dir, ".quailegg", "assets", "v0.1.0")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatalf("assets dir: %v", err)
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "demo_0.1.0_") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the release archive is missing from %s", assetsDir)
	}
	if current, err := os.ReadFile(filepath.Join(dir, "cmd", "demo", "main.go")); err != nil || !strings.Contains(string(current), "vB") {
		t.Fatalf("the working tree must keep its own content: %v", err)
	}
}

// TestLocateSourceFollowsAMergeCommit covers the other merge strategy: the
// release branch is merged with a merge commit instead of being squashed, so
// the snapshot is the merge commit on the target branch, not the branch commit.
func TestLocateSourceFollowsAMergeCommit(t *testing.T) {
	dir := sourceRepo(t, "vA")
	gitRun(t, dir, "branch", "-M", "main")
	gitRun(t, dir, "checkout", "-q", "-b", "release")
	if err := state.SaveMarker(dir, releaseMarker()); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	branchCommit := commitAll(t, dir, "chore(release): v0.1.0")
	gitRun(t, dir, "checkout", "-q", "main")
	gitRun(t, dir, "merge", "--no-ff", "-m", "Merge pull request #7 from release", "release")
	mergeCommit := gitOutput(t, dir, "rev-parse", "HEAD")
	if mergeCommit == branchCommit {
		t.Fatal("the fixture must create a merge commit")
	}

	d := assetDeps(t, dir, nil)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want publish", plan.Phase)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	points, err := d.Git.TagPointsAt(ctx, "v0.1.0", mergeCommit)
	if err != nil || !points {
		t.Fatalf("v0.1.0 must point at the merge commit %s: %v %v", mergeCommit, points, err)
	}
}

// TestReleaseSnapshotStopsWhenAmbiguous pins the refusal: a marker introduced
// twice cannot name one snapshot, so nothing is tagged or built.
func TestReleaseSnapshotStopsWhenAmbiguous(t *testing.T) {
	dir := sourceRepo(t, "vA")
	first := pendingRelease(t, dir)
	if err := os.Remove(state.MarkerPath(dir)); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "chore: drop the marker")
	if err := state.SaveMarker(dir, releaseMarker()); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	second := commitAll(t, dir, "chore: restore the marker")

	d := assetDeps(t, dir, nil)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want publish", plan.Phase)
	}
	err = Run(ctx, d, plan)
	if err == nil {
		t.Fatal("two commits carrying the same marker must stop the release")
	}
	if !strings.Contains(err.Error(), shortSHA(first)) || !strings.Contains(err.Error(), shortSHA(second)) {
		t.Fatalf("the error must name both candidates, got %v", err)
	}
	assertNoExternalWrites(t, d, dir)
}

// TestRecordedSnapshotBeatsAmbiguity keeps the record authoritative: it names
// the snapshot even when the history alone would be ambiguous.
func TestRecordedSnapshotBeatsAmbiguity(t *testing.T) {
	dir := sourceRepo(t, "vA")
	first := pendingRelease(t, dir)
	if err := os.Remove(state.MarkerPath(dir)); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "chore: drop the marker")
	if err := state.SaveMarker(dir, releaseMarker()); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	commitAll(t, dir, "chore: restore the marker")

	d := assetDeps(t, dir, nil)
	ctx := context.Background()
	id, err := IdentityFor(d, &Plan{})
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if _, err := recordSource(ctx, d, id, first, state.SourceOriginCommit); err != nil {
		t.Fatalf("recordSource: %v", err)
	}
	pinned, err := LocateSource(ctx, d, id)
	if err != nil {
		t.Fatalf("LocateSource: %v", err)
	}
	if pinned.SourceCommit != first {
		t.Fatalf("source = %s, want the recorded %s", pinned.SourceCommit, first)
	}
}

// TestPinnedSourceMustCarryTheMarker rejects a caller-pinned commit that does
// not hold this release, and accepts the one that does.
func TestPinnedSourceMustCarryTheMarker(t *testing.T) {
	dir := sourceRepo(t, "vA")
	// A commit before the release carries no marker, so it cannot be the source.
	other := gitOutput(t, dir, "rev-parse", "HEAD")
	release := pendingRelease(t, dir)
	addGoBanner(t, dir, "vB")
	commitAll(t, dir, "feat: move on")

	d := assetDeps(t, dir, nil)
	ctx := context.Background()
	id, err := IdentityFor(d, &Plan{})
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	if _, err := PinSource(ctx, d, id, other); err == nil {
		t.Fatal("a commit without the release marker must be refused")
	}
	pinned, err := PinSource(ctx, d, id, release)
	if err != nil {
		t.Fatalf("PinSource: %v", err)
	}
	if pinned.SourceCommit != release {
		t.Fatalf("source = %s, want %s", pinned.SourceCommit, release)
	}
	if _, err := PinSource(ctx, d, id, "does-not-exist"); err == nil {
		t.Fatal("an unknown revision must be refused")
	}
}

// TestReleaseRefusesADirtyWorkingTreeSnapshot covers the second guard: when the
// checked-out tree is the snapshot, a leftover change would be built into the
// release, so the run stops instead of publishing the wrong content.
func TestReleaseRefusesADirtyWorkingTreeSnapshot(t *testing.T) {
	dir := sourceRepo(t, "vA")
	pendingRelease(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := assetDeps(t, dir, nil)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want publish", plan.Phase)
	}
	err = Run(ctx, d, plan)
	if err == nil || !strings.Contains(err.Error(), "commit or discard") {
		t.Fatalf("a dirty snapshot must stop the release, got %v", err)
	}
	assertNoExternalWrites(t, d, dir)

	// --allow-dirty is the documented escape hatch and still publishes.
	d.Options.AllowDirty = true
	plan, err = Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run with --allow-dirty: %v", err)
	}
	exists, err := d.Git.TagExists(ctx, "v0.1.0")
	if err != nil || !exists {
		t.Fatalf("tag v0.1.0 missing: %v", err)
	}
}

// demoPom is a minimal Maven module the java adapter inspects and deploys.
const demoPom = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>0.1.0</version>
  <packaging>jar</packaging>
</project>
`

// javaSourceRepo prepares a Maven module whose content names its commit.
func javaSourceRepo(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	writeGoFile(t, filepath.Join(dir, "pom.xml"), demoPom)
	writeGoFile(t, filepath.Join(dir, "content.txt"), content+"\n")
	writeGoFile(t, filepath.Join(dir, ".changes", "README.md"), "# Change records\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "init")
	return dir
}

// javaMarker is the marker of a pending Maven release that has no tag yet.
func javaMarker() *state.Marker {
	return &state.Marker{
		SchemaVersion: 1,
		Version:       "0.1.0",
		Mode:          config.ModeCommit,
		Fingerprint:   "sha256:java-source",
		Packages: []state.MarkerPackage{{
			Name: "demo", Path: ".", Resolver: "java",
			Current: "0.1.0", Next: "0.1.0", Bump: "minor", Publish: true,
		}},
		Releases: []state.MarkerRelease{{Version: "0.1.0", Tag: "v0.1.0", Packages: []string{"demo"}}},
	}
}

func javaDeps(t *testing.T, dir string) *Deps {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	ctx := context.Background()
	repo, err := git.Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	publish := true
	cfg.Packages = map[string]config.Package{"demo": {Path: ".", Resolver: config.ResolverJava, Publish: &publish}}
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Verify.Commands = nil
	cfg.Verify.Timeout = "5m"
	return &Deps{
		Root: dir, Cfg: cfg, Git: repo, Run: execx.OSRunner{}, UI: ui.Discard(),
		Now: time.Now, HTTP: &http.Client{Timeout: 5 * time.Second},
		Options: Options{SkipVerify: true, NoPush: true, NoAssets: true, NoGitHubRelease: true},
	}
}

// installFakeMaven puts a stub mvn on PATH that records where the deploy ran and
// the content it found there, so the publish root can be inspected offline.
func installFakeMaven(t *testing.T) (dirLog, contentLog string) {
	t.Helper()
	dir := t.TempDir()
	dirLog = filepath.Join(dir, "mvn.dir")
	contentLog = filepath.Join(dir, "mvn.content")
	t.Setenv("QEGG_MVN_DIR", dirLog)
	t.Setenv("QEGG_MVN_CONTENT", contentLog)
	var script string
	if runtime.GOOS == "windows" {
		script = "@echo off\r\ncd > \"%QEGG_MVN_DIR%\"\r\ntype content.txt > \"%QEGG_MVN_CONTENT%\"\r\nexit /b 0\r\n"
		if err := os.WriteFile(filepath.Join(dir, "mvn.cmd"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script = "#!/bin/sh\npwd > \"$QEGG_MVN_DIR\"\ncat content.txt > \"$QEGG_MVN_CONTENT\"\n"
		if err := os.WriteFile(filepath.Join(dir, "mvn"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dirLog, contentLog
}

// TestPublishRunsInsideTheReleaseSnapshot is the upload half of the A/B case:
// the registry command must run in the snapshot checkout and see A's content,
// not in the moved working tree. Java needs no registry query, so the deploy is
// observable offline.
func TestPublishRunsInsideTheReleaseSnapshot(t *testing.T) {
	dir := javaSourceRepo(t, "vA")
	dirLog, contentLog := installFakeMaven(t)
	marker := javaMarker()
	// A marker written by this qegg: the publish decision may seed the record.
	marker.SchemaVersion = state.MarkerSchemaVersion
	if err := state.SaveMarker(dir, marker); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	release := commitAll(t, dir, "chore(release): v0.1.0")
	writeGoFile(t, filepath.Join(dir, "content.txt"), "vB\n")
	head := commitAll(t, dir, "feat: move on")
	// The publish decision reads the remote tag before it deploys, so the
	// fixture needs an origin even though this run does not push.
	bare := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, dir, "init", "--bare", "-q", bare)
	gitRun(t, dir, "remote", "add", "origin", bare)
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")

	d := javaDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want publish", plan.Phase)
	}
	if len(plan.Packages) != 1 || !plan.Packages[0].Publish {
		t.Fatalf("the plan must publish the package: %+v", plan.Packages)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	published, err := os.ReadFile(dirLog)
	if err != nil {
		t.Fatalf("the deploy never ran: %v", err)
	}
	deployDir := strings.TrimSpace(string(published))
	if deployDir == "" {
		t.Fatal("the deploy did not report its directory")
	}
	if filepath.Clean(deployDir) == filepath.Clean(dir) {
		t.Fatalf("the deploy ran in the moved working tree: %s", deployDir)
	}
	content, err := os.ReadFile(contentLog)
	if err != nil {
		t.Fatalf("the deploy content: %v", err)
	}
	if strings.TrimSpace(string(content)) != "vA" {
		t.Fatalf("the deploy saw %q, want the release snapshot content", content)
	}
	points, err := d.Git.TagPointsAt(ctx, "v0.1.0", release)
	if err != nil || !points {
		t.Fatalf("v0.1.0 must point at the release commit: %v %v", points, err)
	}
	if after := gitOutput(t, dir, "rev-parse", "HEAD"); after != head {
		t.Fatalf("the publish phase must not move the branch: %s -> %s", head, after)
	}
	last, err := state.LatestRun(dir)
	if err != nil || last == nil {
		t.Fatalf("run record: %+v %v", last, err)
	}
	if got := last.Publish["demo"]; got.Status != state.StatusDone {
		t.Fatalf("package state = %+v, want done", got)
	}
}

// TestRemoteTagConflictStopsBeforeAnyWrite pins the early conflict check: a
// remote tag that names another commit stops the run before the assets build and
// before the deploy.
func TestRemoteTagConflictStopsBeforeAnyWrite(t *testing.T) {
	dir := javaSourceRepo(t, "vA")
	dirLog, _ := installFakeMaven(t)
	if err := state.SaveMarker(dir, javaMarker()); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	commitAll(t, dir, "chore(release): v0.1.0")
	writeGoFile(t, filepath.Join(dir, "content.txt"), "vB\n")
	other := commitAll(t, dir, "feat: move on")

	bare := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, dir, "init", "--bare", "-q", bare)
	gitRun(t, dir, "remote", "add", "origin", bare)
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	// The remote already has the release tag, pointing at another commit.
	gitRun(t, dir, "update-ref", "refs/tags/v0.1.0", other)
	gitRun(t, dir, "push", "-q", "origin", "refs/tags/v0.1.0")
	gitRun(t, dir, "tag", "-d", "v0.1.0")

	d := javaDeps(t, dir)
	d.Options.NoPush = false
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = Run(ctx, d, plan)
	if err == nil {
		t.Fatal("a remote tag that names another commit must stop the release")
	}
	if !strings.Contains(err.Error(), "remote tag") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dirLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no deploy may run after the conflict: %v", err)
	}
	if exists, err := d.Git.TagExists(ctx, "v0.1.0"); err != nil || exists {
		t.Fatalf("the local tag must not be created: %v %v", exists, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".quailegg", "assets", "v0.1.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no assets may be built after the conflict: %v", err)
	}
}

// TestSyncReleaseBranchWithoutSnapshot keeps the branch alone when the run never
// pinned a snapshot: flattening it onto HEAD could publish the wrong tree.
func TestSyncReleaseBranchWithoutSnapshot(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	d.Options.NoPush = false
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, dir, "init", "--bare", "-q", origin)
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/master")
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/release")
	before := gitOutput(t, dir, "ls-remote", "--heads", "origin", "refs/heads/release")

	e := &executor{d: d, plan: &Plan{}, run: state.NewRunRecord("test", d.Cfg.Release.Mode, PhasePublish, "")}
	if err := e.syncReleaseBranch(context.Background()); err != nil {
		t.Fatalf("sync without a snapshot must not fail: %v", err)
	}
	after := gitOutput(t, dir, "ls-remote", "--heads", "origin", "refs/heads/release")
	if before != after {
		t.Fatalf("the release branch moved without a pinned snapshot: %q -> %q", before, after)
	}
}

// TestLocateSourceSkipsABrokenHistoricalMarker keeps one unreadable marker in
// the past from breaking the lookup: the commit that introduced the current
// marker still names the snapshot.
func TestLocateSourceSkipsABrokenHistoricalMarker(t *testing.T) {
	dir := sourceRepo(t, "vA")
	writeGoFile(t, filepath.Join(dir, ".quailegg", "release.json"), "{not json\n")
	commitAll(t, dir, "chore: break the marker")
	release := pendingRelease(t, dir)

	d := assetDeps(t, dir, nil)
	ctx := context.Background()
	id, err := IdentityFor(d, &Plan{})
	if err != nil {
		t.Fatalf("IdentityFor: %v", err)
	}
	pinned, err := LocateSource(ctx, d, id)
	if err != nil {
		t.Fatalf("LocateSource: %v", err)
	}
	if pinned.SourceCommit != release {
		t.Fatalf("source = %s, want %s", pinned.SourceCommit, release)
	}
}

// assertNoExternalWrites checks that a stopped release tagged, built and
// published nothing.
func assertNoExternalWrites(t *testing.T, d *Deps, dir string) {
	t.Helper()
	ctx := context.Background()
	exists, err := d.Git.TagExists(ctx, "v0.1.0")
	if err != nil {
		t.Fatalf("TagExists: %v", err)
	}
	if exists {
		t.Fatal("a stopped release must not create the tag")
	}
	if _, err := os.Stat(filepath.Join(dir, ".quailegg", "assets", "v0.1.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a stopped release must not build assets: %v", err)
	}
}
