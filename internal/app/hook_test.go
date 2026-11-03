package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
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
	return hookRepoConfig(t, "commands = [\""+verifyCommand+"\"]\ntimeout = \"1m\"")
}

// hookRepoConfig is hookRepo with a full [verify] section body, so tests can
// exercise hook, hook_commands and hook_timeout.
func hookRepoConfig(t *testing.T, verifyBody string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
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
	config := "schema_version = 1\n[changes]\ndir = \".changes\"\nscope = [\"src\"]\n\n[verify]\n" + verifyBody + "\n\n[packages.demo]\npath = \".\"\nresolver = \"npm\"\n"
	writeHookFile(t, filepath.Join(dir, ".quailegg.toml"), config)
	writeHookFile(t, filepath.Join(dir, ".changes", "change.md"), "---\ntype: feat\npackages:\n  demo: patch\n---\n\nA change.\n")
	run("add", "-A")
	return dir
}

func stageHookSource(t *testing.T, dir string) {
	t.Helper()
	writeHookFile(t, filepath.Join(dir, "src", "demo.js"), "export const demo = 1\n")
	hookGit(t, dir, "add", "-A")
}

func TestHookPreCommitRunsVerification(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepo(t, "exit 1")
	stageHookSource(t, dir)
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
	stageHookSource(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("HookPreCommit: %v", err)
	}
}

func TestSanitizeHookEnv(t *testing.T) {
	for _, name := range execx.GitHookEnvVars() {
		t.Setenv(name, "leaked")
	}
	execx.SanitizeGitHookEnv()
	for _, name := range execx.GitHookEnvVars() {
		if value := os.Getenv(name); value != "" {
			t.Fatalf("%s was not cleared: %q", name, value)
		}
	}
}

func hookGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// hookProjectRepo builds a committed repository whose config records the given
// project identity and whose origin points at remoteURL.
func hookProjectRepo(t *testing.T, remoteURL, configRepo string) string {
	return hookProjectRepoConfig(t, remoteURL, configRepo, "commands = []\ntimeout = \"1m\"")
}

// hookProjectRepoConfig is hookProjectRepo with a full [verify] section body.
func hookProjectRepoConfig(t *testing.T, remoteURL, configRepo, verifyBody string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	hookGit(t, dir, "init", "-q", "-b", "main")
	hookGit(t, dir, "config", "user.email", "hook@example.com")
	hookGit(t, dir, "config", "user.name", "hook")
	cfg := "schema_version = 1\n[project]\ndefault_branch = \"main\"\n"
	if configRepo != "" {
		cfg += "repo = \"" + configRepo + "\"\n"
	}
	cfg += "\n[changes]\ndir = \".changes\"\nscope = [\"cmd\"]\n\n[verify]\n" + verifyBody + "\n\n[packages.demo]\npath = \".\"\nresolver = \"npm\"\n"
	writeHookFile(t, filepath.Join(dir, ".quailegg.toml"), cfg)
	writeHookFile(t, filepath.Join(dir, "package.json"), "{\n  \"name\": \"demo\",\n  \"version\": \"1.0.0\"\n}\n")
	hookGit(t, dir, "add", "-A")
	hookGit(t, dir, "commit", "-qm", "init")
	if remoteURL != "" {
		hookGit(t, dir, "remote", "add", "origin", remoteURL)
	}
	return dir
}

func stageHookScope(t *testing.T, dir string) {
	t.Helper()
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	hookGit(t, dir, "add", "-A")
}

func TestHookPreCommitRequiresRecordInProjectClone(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookProjectRepo(t, "https://github.com/example/demo", "github.com/example/demo")
	stageHookScope(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err == nil {
		t.Fatal("expected a missing-record error in the project clone")
	}
}

func TestHookPreCommitSkipsRecordRuleInForkClone(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookProjectRepo(t, "https://github.com/someone/demo", "github.com/example/demo")
	stageHookScope(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("a fork clone must be allowed to commit: %v", err)
	}
}

func TestHookPreCommitSkipsRecordRuleOutsideDefaultBranch(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookProjectRepo(t, "https://github.com/example/demo", "github.com/example/demo")
	hookGit(t, dir, "checkout", "-q", "-b", "feat")
	stageHookScope(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("a feature branch must be allowed to commit: %v", err)
	}
}

func TestHookPreCommitSkipsVerificationWithoutScopedFiles(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepo(t, "exit 1")
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("a commit without scoped files must skip verification: %v", err)
	}
}

func TestHookPreCommitSkipsVerificationForDocsOnly(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepo(t, "exit 1")
	writeHookFile(t, filepath.Join(dir, "README.md"), "# demo\n")
	hookGit(t, dir, "add", "-A")
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("a docs-only commit must skip verification: %v", err)
	}
}

func TestHookPreCommitSkipsVerificationWhenDisabled(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepoConfig(t, "commands = [\"exit 1\"]\ntimeout = \"1m\"\nhook = false")
	stageHookSource(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("a disabled commit gate must allow the commit: %v", err)
	}
}

func TestHookPreCommitRunsHookCommands(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepoConfig(t, "commands = [\"exit 1\"]\nhook_commands = [\"exit 0\"]\ntimeout = \"1m\"")
	stageHookSource(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err != nil {
		t.Fatalf("the hook must use hook_commands instead of commands: %v", err)
	}
}

func TestHookPreCommitFailsOnHookCommands(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRepoConfig(t, "commands = [\"exit 0\"]\nhook_commands = [\"exit 1\"]\ntimeout = \"1m\"")
	stageHookSource(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = a.HookPreCommit(context.Background())
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected a hook_commands failure, got %v", err)
	}
}

func TestHookPreCommitDisabledStillRequiresRecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookProjectRepoConfig(t, "https://github.com/example/demo", "github.com/example/demo", "commands = []\ntimeout = \"1m\"\nhook = false")
	stageHookScope(t, dir)
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.HookPreCommit(context.Background()); err == nil {
		t.Fatal("turning the verification gate off must not disable the change record rule")
	}
}
