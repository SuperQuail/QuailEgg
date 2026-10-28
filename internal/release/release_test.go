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
