package release

import (
	"context"
	"net/http"
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

func writeGoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newGoRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	writeGoFile(t, filepath.Join(dir, "go.mod"), "module example.com/demo\n\ngo 1.21\n")
	writeGoFile(t, filepath.Join(dir, "demo.go"), "package demo\n\nconst Name = \"demo\"\n")
	writeGoFile(t, filepath.Join(dir, ".changes", "README.md"), "# Change records\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "init")
	return dir
}

// commitGoRecord writes a change record for the Go module and commits it, so a
// release run starts from a clean working tree.
func commitGoRecord(t *testing.T, dir, name, bump string) {
	t.Helper()
	record := "---\ntype: feat\npackages:\n  example.com/demo: " + bump + "\n---\n\nAdd a feature.\n"
	writeGoFile(t, filepath.Join(dir, ".changes", name), record)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "feat: add feature")
}

func tagGo(t *testing.T, dir, tag string) {
	t.Helper()
	gitRun(t, dir, "tag", "-a", tag, "-m", "release "+tag)
}

func goDeps(t *testing.T, dir string) *Deps {
	t.Helper()
	ctx := context.Background()
	repo, err := git.Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	publish := true
	cfg.Packages = map[string]config.Package{
		"example.com/demo": {Path: ".", Resolver: config.ResolverGo, Publish: &publish},
	}
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

func TestGoVersionPhaseUsesLatestTag(t *testing.T) {
	dir := newGoRepo(t)
	d := goDeps(t, dir)
	ctx := context.Background()
	tagGo(t, dir, "v1.2.3")
	commitGoRecord(t, dir, "add-feature.md", "minor")

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion {
		t.Fatalf("phase = %q, want version", plan.Phase)
	}
	if plan.Version.Current != "1.2.3" || plan.Version.Next != "1.3.0" {
		t.Fatalf("unexpected version: %+v", plan.Version)
	}
	if plan.Tag != "v1.3.0" || plan.PreviousTag != "v1.2.3" {
		t.Fatalf("unexpected tags: next=%q previous=%q", plan.Tag, plan.PreviousTag)
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	exists, err := d.Git.TagExists(ctx, "v1.3.0")
	if err != nil || !exists {
		t.Fatalf("tag v1.3.0 missing: %v", err)
	}
	changelog, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changelog), "## v1.3.0") {
		t.Fatalf("unexpected changelog:\n%s", changelog)
	}
	marker, err := os.ReadFile(state.MarkerPath(dir))
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if !strings.Contains(string(marker), "\"version\": \"1.3.0\"") {
		t.Fatalf("unexpected marker:\n%s", marker)
	}

	plan2, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after release: %v", err)
	}
	if plan2.Phase != PhaseNone {
		t.Fatalf("expected a no-op after release, got %q", plan2.Phase)
	}
}

func TestGoFirstReleaseStartsAtZero(t *testing.T) {
	dir := newGoRepo(t)
	d := goDeps(t, dir)
	writeGoFile(t, filepath.Join(dir, ".changes", "first.md"), "---\ntype: feat\npackages:\n  example.com/demo: minor\n---\n\nFirst release.\n")

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Current != "0.0.0" || plan.Version.Next != "0.1.0" {
		t.Fatalf("unexpected version: %+v", plan.Version)
	}
	if plan.PreviousTag != "" {
		t.Fatalf("a first release must not link to a previous tag, got %q", plan.PreviousTag)
	}
}

func TestGoPublishPhaseAfterMarker(t *testing.T) {
	dir := newGoRepo(t)
	d := goDeps(t, dir)
	ctx := context.Background()
	tagGo(t, dir, "v1.0.0")
	if err := state.SaveMarker(dir, &state.Marker{
		SchemaVersion: 1,
		Version:       "1.1.0",
		Mode:          config.ModePR,
		Packages: []state.MarkerPackage{{
			Name: "example.com/demo", Path: ".", Resolver: "go",
			Current: "1.0.0", Next: "1.1.0", Bump: "minor", Publish: true,
		}},
	}); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
	head, err := d.Git.HeadSHA(ctx)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhasePublish {
		t.Fatalf("phase = %q, want publish", plan.Phase)
	}
	if plan.Version.Current != "1.0.0" || plan.Version.Next != "1.1.0" || plan.Tag != "v1.1.0" {
		t.Fatalf("unexpected plan: %+v %q", plan.Version, plan.Tag)
	}
	if plan.HasChanges {
		t.Fatal("a publish phase must not consume change records again")
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	exists, err := d.Git.TagExists(ctx, "v1.1.0")
	if err != nil || !exists {
		t.Fatalf("tag v1.1.0 missing: %v", err)
	}
	after, err := d.Git.HeadSHA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after != head {
		t.Fatalf("publish phase must not commit: head %s -> %s", head, after)
	}
}

func TestGoMajorBumpRejected(t *testing.T) {
	dir := newGoRepo(t)
	d := goDeps(t, dir)
	tagGo(t, dir, "v1.0.0")
	writeGoFile(t, filepath.Join(dir, ".changes", "breaking-change.md"), "---\ntype: feat\npackages:\n  example.com/demo: major\n---\n\nBreak everything.\n")

	_, err := Build(context.Background(), d)
	if err == nil || !strings.Contains(err.Error(), "/v2") {
		t.Fatalf("expected a /v2 module path error, got %v", err)
	}
}

func TestGoTagPrefixWarning(t *testing.T) {
	dir := newGoRepo(t)
	d := goDeps(t, dir)
	d.Cfg.Git.TagPrefix = "release-"
	writeGoFile(t, filepath.Join(dir, ".changes", "add-feature.md"), "---\ntype: feat\npackages:\n  example.com/demo: minor\n---\n\nAdd a feature.\n")

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "release-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a tag prefix warning, got %v", plan.Warnings)
	}
}
