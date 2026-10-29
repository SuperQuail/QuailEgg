package app

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

func preGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newPreApp(t *testing.T) *App {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	preGit(t, dir, "init", "-q")
	preGit(t, dir, "config", "user.email", "pre@example.com")
	preGit(t, dir, "config", "user.name", "pre")
	manifest := `{
  "name": "pre-demo",
  "version": "1.0.0"
}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := `---
type: feat
packages:
  pre-demo: minor
---

Add a feature.
`
	if err := os.WriteFile(filepath.Join(dir, ".changes", "add-feature.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preGit(t, dir, "add", "-A")
	preGit(t, dir, "commit", "-qm", "feat: add feature")

	run := execx.OSRunner{}
	repo, err := git.Open(context.Background(), dir, run)
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{"pre-demo": {Path: ".", Resolver: config.ResolverNPM}}
	cfg.Release.Mode = config.ModeCommit
	cfg.Verify.Commands = nil
	return &App{
		Root: dir,
		Cfg:  cfg,
		Run:  run,
		Git:  repo,
		UI:   ui.Discard(),
		Now:  time.Now,
		HTTP: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestPreEntersAndPreviews(t *testing.T) {
	a := newPreApp(t)
	ctx := context.Background()

	status, err := a.Pre(ctx, PreOptions{})
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if status.Channel != "" || status.Next != "1.1.0" {
		t.Fatalf("inactive status = %+v, want no channel and 1.1.0", status)
	}

	entered, err := a.Pre(ctx, PreOptions{Channel: "alpha"})
	if err != nil {
		t.Fatalf("Pre enter: %v", err)
	}
	if !entered.Changed || entered.Channel != "alpha" || entered.Next != "1.1.0-alpha.1" {
		t.Fatalf("enter result = %+v", entered)
	}

	again, err := a.Pre(ctx, PreOptions{Channel: "alpha"})
	if err != nil {
		t.Fatalf("Pre enter again: %v", err)
	}
	if again.Changed {
		t.Fatalf("re-entering the same channel should not change anything: %+v", again)
	}

	if _, err := a.Pre(ctx, PreOptions{Channel: "beta"}); err == nil || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("expected a busy-channel error, got %v", err)
	}
}

func TestPreExitReturnsToStable(t *testing.T) {
	a := newPreApp(t)
	ctx := context.Background()
	if _, err := a.Pre(ctx, PreOptions{Channel: "rc"}); err != nil {
		t.Fatalf("Pre enter: %v", err)
	}
	left, err := a.Pre(ctx, PreOptions{Exit: true})
	if err != nil {
		t.Fatalf("Pre exit: %v", err)
	}
	if !left.Changed || left.Channel != "" || left.Next != "1.1.0" {
		t.Fatalf("exit result = %+v", left)
	}
	if _, err := a.Pre(ctx, PreOptions{Exit: true}); err == nil {
		t.Fatal("expected an error when no channel is active")
	}
}

func TestPreRejectsUnknownChannel(t *testing.T) {
	a := newPreApp(t)
	if _, err := a.Pre(context.Background(), PreOptions{Channel: "nightly"}); err == nil {
		t.Fatal("expected an error for an unsupported channel")
	}
}
