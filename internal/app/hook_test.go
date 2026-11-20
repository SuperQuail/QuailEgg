package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/changes"
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

// Record fixtures for the hook: the "demo" package is registered in the
// hookProjectRepo configuration.
const (
	// hookValidRecord satisfies the rule for the registered package.
	hookValidRecord = `---
type: feat
packages:
  demo: patch
---

Describe the change the commit carries.
`
	// hookBrokenRecord has no front matter at all.
	hookBrokenRecord = `not a record at all
`
	// hookBadTypeRecord carries a type the validator rejects.
	hookBadTypeRecord = `---
type: nope
packages:
  demo: patch
---

Describe the change the commit carries.
`
	// hookUnknownPackageRecord names a package the fixture does not register.
	hookUnknownPackageRecord = `---
type: feat
packages:
  other: patch
---

Reference a package that is not registered.
`
)

// hookHead returns the commit the repository currently points at.
func hookHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// commitHookRecord commits a record, so a later commit can delete or rename it.
func commitHookRecord(t *testing.T, dir, name, body string) {
	t.Helper()
	writeHookFile(t, filepath.Join(dir, ".changes", name), body)
	hookGit(t, dir, "add", "-A")
	hookGit(t, dir, "commit", "-qm", "feat: add a record")
}

// hookRuleRepo is a project clone on its default branch, the only place the
// record rule runs.
func hookRuleRepo(t *testing.T) string {
	t.Helper()
	return hookProjectRepoConfig(t, "https://github.com/example/demo", "github.com/example/demo", "commands = []\ntimeout = \"1m\"")
}

func hookRun(t *testing.T, dir string) error {
	t.Helper()
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a.HookPreCommit(context.Background())
}

// A valid record in the index judges the commit, not the worktree copy the author
// edited after staging.
func TestHookPreCommitReadsTheStagedRecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeHookFile(t, filepath.Join(dir, ".changes", "index-fix.md"), hookValidRecord)
	hookGit(t, dir, "add", "-A")
	writeHookFile(t, filepath.Join(dir, ".changes", "index-fix.md"), hookBadTypeRecord)
	if err := hookRun(t, dir); err != nil {
		t.Fatalf("the staged record is valid, the worktree copy is not: %v", err)
	}
}

// Repairing the worktree copy of a broken staged record must not hide it.
func TestHookPreCommitRejectsTheUnstagedFix(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeHookFile(t, filepath.Join(dir, ".changes", "index-fix.md"), hookBadTypeRecord)
	hookGit(t, dir, "add", "-A")
	writeHookFile(t, filepath.Join(dir, ".changes", "index-fix.md"), hookValidRecord)
	err := hookRun(t, dir)
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("the staged record is invalid, the worktree copy is not: got %v", err)
	}
}

// Removing the only record while touching a scoped file must not satisfy the rule.
func TestHookPreCommitRejectsARecordDeletion(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	commitHookRecord(t, dir, "old-fix.md", hookValidRecord)
	hookGit(t, dir, "rm", "-q", ".changes/old-fix.md")
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	hookGit(t, dir, "add", "-A")
	err := hookRun(t, dir)
	if err == nil {
		t.Fatal("deleting the only record must not satisfy the rule")
	}
	var missing *changes.MissingRecordError
	if !errors.As(err, &missing) {
		t.Fatalf("want a missing record error, got %T: %v", err, err)
	}
}

// The same source change passes once the commit adds a valid record.
func TestHookPreCommitAcceptsANewRecordNextToADeletedOne(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	commitHookRecord(t, dir, "old-fix.md", hookValidRecord)
	hookGit(t, dir, "rm", "-q", ".changes/old-fix.md")
	writeHookFile(t, filepath.Join(dir, ".changes", "new-fix.md"), hookValidRecord)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	hookGit(t, dir, "add", "-A")
	if err := hookRun(t, dir); err != nil {
		t.Fatalf("a valid added record must satisfy the rule: %v", err)
	}
}

// A rename arrives as a deletion plus an addition because the staged diff has
// rename detection disabled: the record at its new path still counts.
func TestHookPreCommitAcceptsARenamedRecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	commitHookRecord(t, dir, "old-fix.md", hookValidRecord)
	hookGit(t, dir, "mv", ".changes/old-fix.md", ".changes/new-fix.md")
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	hookGit(t, dir, "add", "-A")
	if err := hookRun(t, dir); err != nil {
		t.Fatalf("a renamed record is still a record: %v", err)
	}
}

// The release pipeline reads one level of the record directory, so a record in a
// subdirectory must not stand in for one it will never read. The pull request
// check refuses it the same way.
func TestHookPreCommitRejectsANestedRecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeHookFile(t, filepath.Join(dir, ".changes", "nested", "deep-fix.md"), hookValidRecord)
	hookGit(t, dir, "add", "-A")
	err := hookRun(t, dir)
	var missing *changes.MissingRecordError
	if !errors.As(err, &missing) {
		t.Fatalf("a record outside the record directory must not satisfy the rule: %v", err)
	}
}

// A change outside the configured scope needs no record.
func TestHookPreCommitAllowsDocsOnly(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "notes.md"), "notes\n")
	hookGit(t, dir, "add", "-A")
	if err := hookRun(t, dir); err != nil {
		t.Fatalf("a docs only commit needs no record: %v", err)
	}
}

// Deleting a record while only touching a file qegg manages itself still needs no
// record: the managed file never counts as a scope change.
func TestHookPreCommitAllowsManagedChangeWithDeletedRecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	commitHookRecord(t, dir, "old-fix.md", hookValidRecord)
	hookGit(t, dir, "rm", "-q", ".changes/old-fix.md")
	writeHookFile(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	hookGit(t, dir, "add", "-A")
	if err := hookRun(t, dir); err != nil {
		t.Fatalf("a managed-file only commit needs no record: %v", err)
	}
}

// A record staged with git add -N is not in the index yet, so it cannot satisfy
// the rule.
func TestHookPreCommitIgnoresIntentToAdd(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeHookFile(t, filepath.Join(dir, ".changes", "intent-fix.md"), hookValidRecord)
	hookGit(t, dir, "add", "cmd/main.go")
	hookGit(t, dir, "add", "-N", ".changes/intent-fix.md")
	if err := hookRun(t, dir); err == nil {
		t.Fatal("an intent-to-add record must not satisfy the rule")
	}
}

// The record is named once, not once per wrapper, so the author sees a readable
// parse error.
func TestHookPreCommitNamesTheRecordOnceOnParseError(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeHookFile(t, filepath.Join(dir, ".changes", "index-fix.md"), hookBrokenRecord)
	hookGit(t, dir, "add", "-A")
	err := hookRun(t, dir)
	if err == nil {
		t.Fatal("a record without front matter must be rejected")
	}
	if want := ".changes/index-fix.md: "; strings.Count(err.Error(), want) != 1 {
		t.Fatalf("the record should be named once: %q", err.Error())
	}
}

// A commit that deletes a scoped source file is a scope change too: the pull
// request check has always counted it, and the hook now agrees instead of hiding
// deletions behind a name-only diff.
func TestHookPreCommitCountsADeletedSourceFile(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	hookGit(t, dir, "add", "-A")
	hookGit(t, dir, "commit", "-qm", "feat: add source")
	hookGit(t, dir, "rm", "-q", "cmd/main.go")
	if err := hookRun(t, dir); err == nil {
		t.Fatal("deleting a scoped source file without a record must be refused")
	}
}

// git commit -a exports GIT_INDEX_FILE pointing at the index the commit will use.
// The rule reads that index, which is why the hook reads the index before it
// sanitises the inherited git environment.
func TestHookPreCommitReadsTheCommitIndex(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir := hookRuleRepo(t)
	index := filepath.Join(t.TempDir(), "next-index.lock")
	data, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatalf("read the repository index: %v", err)
	}
	if err := os.WriteFile(index, data, 0o644); err != nil {
		t.Fatalf("write the commit index: %v", err)
	}
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeHookFile(t, filepath.Join(dir, ".changes", "index-fix.md"), hookBadTypeRecord)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add with GIT_INDEX_FILE: %v\n%s", err, out)
	}
	t.Setenv("GIT_INDEX_FILE", index)
	err = hookRun(t, dir)
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("the hook judged the repository index instead of the commit index: %v", err)
	}
}
