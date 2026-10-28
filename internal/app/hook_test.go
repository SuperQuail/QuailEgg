package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func writeHookFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hookRepo builds a repository with a valid staged change record and the given
// verification command.
func hookRepo(t *testing.T, verifyCommand string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "hook@example.com")
	run("config", "user.name", "hook")
	config := "schema_version = 1\n[verify]\ncommands = [\"" + verifyCommand + "\"]\ntimeout = \"1m\"\n[packages.demo]\npath = \".\"\nresolver = \"npm\"\n"
	writeHookFile(t, filepath.Join(dir, ".quailegg.toml"), config)
	writeHookFile(t, filepath.Join(dir, ".changes", "change.md"), "---\ntype: feat\npackages:\n  demo: patch\n---\n\nA change.\n")
	run("add", "-A")
	return dir
}

func TestHookPreCommitRunsVerification(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepo(t, "exit 1")
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = a.HookPreCommit(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected a verification failure, got %v", err)
	}
}

func TestHookPreCommitPassesWithPassingVerification(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepo(t, "exit 0")
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("HookPreCommit: %v", err)
	}
}
