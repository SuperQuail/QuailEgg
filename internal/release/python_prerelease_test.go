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

func newPythonRepo(t *testing.T, version string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	manifest := "[project]\nname = \"py-demo\"\nversion = \"" + version + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := "---\ntype: fix\npackages:\n  py-demo: patch\n---\n\nFix a bug.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "fix-bug.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "fix: bug")
	return dir
}

func pythonDeps(t *testing.T, dir string) *Deps {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	ctx := context.Background()
	repo, err := git.Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	publish := false
	cfg.Packages = map[string]config.Package{"py-demo": {Path: ".", Resolver: "python", Publish: &publish}}
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
		HTTP:    &http.Client{Timeout: time.Second},
		Options: Options{SkipVerify: true, NoPublish: true, NoPush: true, NoGitHubRelease: true},
	}
}

func TestPlanPythonPreRelease(t *testing.T) {
	dir := newPythonRepo(t, "2.0.2-a14")
	gitRun(t, dir, "tag", "v2.0.2a14")
	if err := state.SavePreRelease(dir, &state.PreRelease{Channel: "alpha"}); err != nil {
		t.Fatalf("SavePreRelease: %v", err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: enter alpha")
	d := pythonDeps(t, dir)
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Channel != "alpha" || plan.Phase != PhaseVersion {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Version.Next != "2.0.2-a15" {
		t.Fatalf("next = %q, want 2.0.2-a15", plan.Version.Next)
	}
	if plan.Tag != "v2.0.2a15" {
		t.Fatalf("tag = %q, want v2.0.2a15", plan.Tag)
	}
	if len(plan.Packages) != 1 || plan.Packages[0].Next != "2.0.2-a15" {
		t.Fatalf("packages = %+v", plan.Packages)
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "2.0.2-a15") {
		t.Fatalf("manifest not bumped: %s", manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatal("a prerelease must not write CHANGELOG.md")
	}
	if _, err := os.Stat(filepath.Join(dir, ".changes", "fix-bug.md")); err != nil {
		t.Fatal("a prerelease must keep the change records")
	}
	lock, err := state.LoadPreRelease(dir)
	if err != nil || lock == nil || lock.Channel != "alpha" || lock.Version != "2.0.2-a15" {
		t.Fatalf("lock = %+v, err = %v", lock, err)
	}
}

func TestPlanPythonPreReleaseLockMismatch(t *testing.T) {
	dir := newPythonRepo(t, "2.0.2-a14")
	if err := state.SavePreRelease(dir, &state.PreRelease{Channel: "alpha"}); err != nil {
		t.Fatalf("SavePreRelease: %v", err)
	}
	// Force a version that does not belong to the active channel.
	lockPath := state.PreReleasePath(dir)
	if err := os.WriteFile(lockPath, []byte("{\"schema_version\":1,\"channel\":\"alpha\",\"version\":\"2.0.2-beta.1\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), pythonDeps(t, dir))
	if err == nil || !strings.Contains(err.Error(), "prerelease lock version") {
		t.Fatalf("Build error = %v, want a lock mismatch", err)
	}
}

func TestPlanPythonPreReleaseUnlockedWarns(t *testing.T) {
	dir := newPythonRepo(t, "2.0.2-a14")
	plan, err := Build(context.Background(), pythonDeps(t, dir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Without a channel lock the manifest prerelease graduates with the next
	// patch record: 2.0.2-a14 -> 2.0.2.
	if plan.Phase != PhaseVersion || plan.Version.Next != "2.0.2" {
		t.Fatalf("plan = %+v", plan)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "prerelease") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want a prerelease warning", plan.Warnings)
	}
}
