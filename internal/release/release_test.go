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
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{\n  \"name\": \"rel-demo\",\n  \"version\": \"1.0.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := "---\ntype: feat\npackages:\n  rel-demo: minor\n---\n\nAdd a feature.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "add-feature.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "feat: add feature")
	return dir
}

func deps(t *testing.T, dir string) *Deps {
	t.Helper()
	// The preflight demands the default branch when it detects CI, so tests
	// pin the environment to a local run.
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	ctx := context.Background()
	repo, err := git.Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	publish := false
	cfg.Packages = map[string]config.Package{"rel-demo": {Path: ".", Resolver: "npm", Publish: &publish}}
	cfg.Release.Mode = config.ModeCommit
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

func TestBuildAndExecuteVersionPhase(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want version", plan.Phase)
	}
	if plan.Version.Next != "1.1.0" || plan.Tag != "v1.1.0" {
		t.Fatalf("unexpected plan: %+v", plan.Version)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Commit == "" {
		t.Fatalf("expected one change with provenance, got %+v", plan.Changes)
	}
	if !strings.HasPrefix(plan.Fingerprint, "sha256:") {
		t.Fatalf("unexpected fingerprint %q", plan.Fingerprint)
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "1.1.0") {
		t.Fatalf("manifest not bumped:\n%s", manifest)
	}
	changelog, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changelog), "## v1.1.0 - ") || !strings.Contains(string(changelog), "Add a feature.") {
		t.Fatalf("unexpected changelog:\n%s", changelog)
	}
	if strings.Contains(string(changelog), ".changes/add-feature.md") {
		t.Fatalf("changelog should not mention record paths:\n%s", changelog)
	}
	markerData, err := os.ReadFile(filepath.Join(dir, ".quailegg", "release.json"))
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if !strings.Contains(string(markerData), "\"version\": \"1.1.0\"") {
		t.Fatalf("unexpected marker:\n%s", markerData)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".changes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "README.md" && strings.HasSuffix(entry.Name(), ".md") {
			t.Fatalf("record was not consumed: %s", entry.Name())
		}
	}
	if _, err := d.Git.TagExists(ctx, "v1.1.0"); err != nil {
		t.Fatalf("tag check: %v", err)
	}
	exists, err := d.Git.TagExists(ctx, "v1.1.0")
	if err != nil || !exists {
		t.Fatalf("tag v1.1.0 missing: %v", err)
	}

	// A second run must be a no-op.
	plan2, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after release: %v", err)
	}
	if plan2.Phase != PhaseNone {
		t.Fatalf("expected no-op after release, got %q", plan2.Phase)
	}
}

func TestFingerprintIsStable(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	ctx := context.Background()
	first, err := Build(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == "" || first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint changed: %q vs %q", first.Fingerprint, second.Fingerprint)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPRModePlanStepsAndBody(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	d.Cfg.Release.Mode = config.ModePR
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want version", plan.Phase)
	}
	var ids []string
	for _, step := range plan.Steps {
		ids = append(ids, step.ID)
	}
	if got := strings.Join(ids, ","); got != "preflight,verify,branch,bump,changelog,commit" {
		t.Fatalf("steps = %s", got)
	}

	body := plan.pullRequestBody(d)
	if !strings.Contains(body, "## v1.1.0 - ") {
		t.Fatalf("body is missing the changelog section:\n%s", body)
	}
	if !strings.Contains(body, "Add a feature.") {
		t.Fatalf("body is missing the change text:\n%s", body)
	}
	if strings.Contains(body, "qegg:release") {
		t.Fatalf("body must not contain release markers:\n%s", body)
	}
}

func TestReleaseRefspecIsForced(t *testing.T) {
	if got := releaseRefspec("release"); got != "+HEAD:refs/heads/release" {
		t.Fatalf("refspec = %q", got)
	}
}

func TestSyncReleaseBranch(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	d.Options.NoPush = false
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, dir, "init", "--bare", "-q", origin)
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/release")

	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "docs: add changelog")
	head := gitOutput(t, dir, "rev-parse", "HEAD")

	e := &executor{d: d, plan: &Plan{}, run: state.NewRunRecord("test", d.Cfg.Release.Mode, PhasePublish, "")}
	ctx := context.Background()
	if err := e.syncReleaseBranch(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	remote := gitOutput(t, dir, "ls-remote", "--heads", "origin", "refs/heads/release")
	if !strings.HasPrefix(remote, head) {
		t.Fatalf("release branch was not synced to %s: %q", head, remote)
	}

	gitRun(t, dir, "push", "-q", "origin", "--delete", "release")
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "docs: update changelog")
	if err := e.syncReleaseBranch(ctx); err != nil {
		t.Fatalf("sync after delete: %v", err)
	}
	if remote := gitOutput(t, dir, "ls-remote", "--heads", "origin", "refs/heads/release"); remote != "" {
		t.Fatalf("deleted branch must stay deleted, got %q", remote)
	}
}

func TestPullRequestNumber(t *testing.T) {
	tests := []struct {
		subject string
		want    int
	}{
		{subject: "feat(cmd): greet twice (#2)", want: 2},
		{subject: "fix: keep the marker (#12)", want: 12},
		{subject: "feat: add greeting", want: 0},
		{subject: "docs: mention (parentheses) only", want: 0},
	}
	for _, tc := range tests {
		if got := pullRequestNumber(tc.subject); got != tc.want {
			t.Errorf("pullRequestNumber(%q) = %d, want %d", tc.subject, got, tc.want)
		}
	}
}

func writeLock(t *testing.T, dir, channel, version string) {
	t.Helper()
	if err := state.SavePreRelease(dir, &state.PreRelease{Channel: channel, Version: version}); err != nil {
		t.Fatalf("SavePreRelease: %v", err)
	}
}

func writeMarker(t *testing.T, dir, version, channel string, recordBodies map[string]string) {
	t.Helper()
	marker := &state.Marker{Version: version, Mode: config.ModePR, Channel: channel}
	for file, body := range recordBodies {
		marker.Changes = append(marker.Changes, state.MarkerChange{File: file, Body: body})
	}
	if err := state.SaveMarker(dir, marker); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
}

func TestPlanPreReleaseFirstCut(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	writeLock(t, dir, "alpha", "")
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want version", plan.Phase)
	}
	if plan.Version.Next != "1.1.0-alpha.1" || plan.Tag != "v1.1.0-alpha.1" {
		t.Fatalf("unexpected version: %+v tag %q", plan.Version, plan.Tag)
	}
	if plan.Channel != "alpha" || plan.Graduating {
		t.Fatalf("unexpected channel state: %q graduating=%v", plan.Channel, plan.Graduating)
	}
}

func TestPlanPreReleaseGuardAndIncrement(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	writeLock(t, dir, "alpha", "1.1.0-alpha.1")
	writeMarker(t, dir, "1.1.0-alpha.1", "alpha", map[string]string{".changes/add-feature.md": "Add a feature."})
	gitRun(t, dir, "tag", "v1.1.0-alpha.1")

	guard, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if guard.Phase != PhaseNone {
		t.Fatalf("phase = %q, want none before a new record", guard.Phase)
	}

	record := "---\ntype: feat\npackages:\n  rel-demo: minor\n---\n\nAdd a bigger feature.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "add-feature.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion || plan.Version.Next != "1.1.0-alpha.2" {
		t.Fatalf("phase = %q next = %q, want version 1.1.0-alpha.2", plan.Phase, plan.Version.Next)
	}
}

func TestPlanGraduatesPreRelease(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	gitRun(t, dir, "tag", "v1.0.0")
	writeMarker(t, dir, "1.1.0-alpha.2", "alpha", map[string]string{".changes/add-feature.md": "Add a feature."})
	gitRun(t, dir, "tag", "v1.1.0-alpha.2")

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want version", plan.Phase)
	}
	if plan.Version.Next != "1.1.0" || !plan.Graduating {
		t.Fatalf("expected graduation to 1.1.0, got %+v graduating=%v", plan.Version, plan.Graduating)
	}
	if plan.Channel != "" {
		t.Fatalf("graduated plan must not carry a channel, got %q", plan.Channel)
	}
}

func TestPlanPreReleaseIdleWithoutRecords(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	if err := os.Remove(filepath.Join(dir, ".changes", "add-feature.md")); err != nil {
		t.Fatal(err)
	}
	writeLock(t, dir, "beta", "")
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseNone {
		t.Fatalf("phase = %q, want none", plan.Phase)
	}
	if plan.Channel != "beta" {
		t.Fatalf("channel = %q, want beta", plan.Channel)
	}
}
