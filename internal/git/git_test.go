package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	return dir
}

func TestIntroducingCommit(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	path := filepath.Join(dir, ".changes", "a.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ntype: feat\npackages:\n  pkg: patch\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "feat: add record")
	first, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	// modifying the file must not change the provenance
	if err := os.WriteFile(path, []byte("---\ntype: feat\npackages:\n  pkg: minor\n---\nbody v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-qam", "chore: tweak record")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sha, err := repo.IntroducingCommit(ctx, ".changes/a.md")
	if err != nil {
		t.Fatalf("IntroducingCommit: %v", err)
	}
	want := string(first[:len(first)-1])
	if sha != want {
		t.Fatalf("IntroducingCommit = %q, want %q", sha, want)
	}
}

func TestTagsAndStatus(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "init")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatal(err)
	}
	clean, err := repo.IsClean(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("repo should be clean")
	}
	if err := repo.CreateTag(ctx, "v1.0.0", "release"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	exists, err := repo.TagExists(ctx, "v1.0.0")
	if err != nil || !exists {
		t.Fatalf("TagExists = %v, %v", exists, err)
	}
	head, err := repo.HeadSHA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	points, err := repo.TagPointsAt(ctx, "v1.0.0", head)
	if err != nil || !points {
		t.Fatalf("TagPointsAt = %v, %v", points, err)
	}
}

func TestIsCleanExcept(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "init")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".quailegg", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".quailegg", "state", "run.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".quailegg", "lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	clean, err := repo.IsClean(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("runtime metadata must make the plain clean check fail")
	}
	clean, err = repo.IsCleanExcept(ctx, []string{".quailegg/state", ".quailegg/lock"})
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("runtime metadata must be excluded from the dirty check")
	}

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err = repo.IsCleanExcept(ctx, []string{".quailegg/state", ".quailegg/lock"})
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("user changes must still be reported")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCommitSubjectAndAuthor(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "feat: add a file")
	sha, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := repo.CommitSubject(ctx, strings.TrimSpace(string(sha)))
	if err != nil || subject != "feat: add a file" {
		t.Fatalf("CommitSubject = %q, %v", subject, err)
	}
	author, err := repo.CommitAuthor(ctx, strings.TrimSpace(string(sha)))
	if err != nil || author != "test" {
		t.Fatalf("CommitAuthor = %q, %v", author, err)
	}
}

func TestCheckoutBranchAndRemoteBranchExists(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "init")
	bare := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, dir, "init", "--bare", "-q", bare)
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckoutBranch(ctx, "release"); err != nil {
		t.Fatalf("CheckoutBranch: %v", err)
	}
	branch, err := repo.Branch(ctx)
	if err != nil || branch != "release" {
		t.Fatalf("Branch = %q, %v", branch, err)
	}
	exists, err := repo.RemoteBranchExists(ctx, "origin", "main")
	if err != nil || !exists {
		t.Fatalf("main should exist: %v %v", exists, err)
	}
	exists, err = repo.RemoteBranchExists(ctx, "origin", "release")
	if err != nil || exists {
		t.Fatalf("release should not exist: %v %v", exists, err)
	}
}

func TestIgnored(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	repo, err := Open(context.Background(), dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\ntracked.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ignored.txt", "tracked.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "add", "-f", "tracked.txt")
	runGit(t, dir, "commit", "-qm", "chore: init")
	ctx := context.Background()
	ignored, err := repo.Ignored(ctx, "ignored.txt")
	if err != nil {
		t.Fatalf("Ignored(ignored.txt): %v", err)
	}
	if !ignored {
		t.Fatal("ignored.txt must be reported as ignored")
	}
	ignored, err = repo.Ignored(ctx, "tracked.txt")
	if err != nil {
		t.Fatalf("Ignored(tracked.txt): %v", err)
	}
	if ignored {
		t.Fatal("tracked files are never ignored")
	}
}
