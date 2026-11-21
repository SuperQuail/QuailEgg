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

// execOutput runs git in dir and returns its standard output.
func execOutput(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}

// commitFile writes content, stages it and commits, returning the new hash.
func commitFile(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", message)
	return strings.TrimSpace(string(mustOutput(t, dir, "rev-parse", "HEAD")))
}

func mustOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	out, err := execOutput(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// TestFileAtCommitSeparatesAbsentFromBroken pins the difference the snapshot
// lookup depends on: a path that is not in a commit is not an error, an
// unreadable revision is.
func TestFileAtCommitSeparatesAbsentFromBroken(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	first := commitFile(t, dir, "a.txt", "alpha\n", "chore: add a")
	second := commitFile(t, dir, "b.txt", "beta\n", "chore: add b")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, found, err := repo.FileAtCommit(ctx, first, "a.txt")
	if err != nil || !found {
		t.Fatalf("FileAtCommit(a.txt at first) = %q found=%v err=%v", data, found, err)
	}
	if strings.TrimSpace(string(data)) != "alpha" {
		t.Fatalf("content = %q", data)
	}

	if _, found, err = repo.FileAtCommit(ctx, first, "b.txt"); err != nil || found {
		t.Fatalf("a path the commit does not have must be absent, not an error: found=%v err=%v", found, err)
	}
	if _, found, err = repo.FileAtCommit(ctx, second, "a.txt"); err != nil || !found {
		t.Fatalf("a file survives into a later commit: found=%v err=%v", found, err)
	}
	if _, _, err = repo.FileAtCommit(ctx, "0000000000000000000000000000000000000000", "a.txt"); err == nil {
		t.Fatal("an unknown revision must be an error, not a missing file")
	}
}

func TestParentCommitAndIsShallow(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	root := commitFile(t, dir, "a.txt", "alpha\n", "chore: root")
	child := commitFile(t, dir, "b.txt", "beta\n", "chore: child")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	parent, ok, err := repo.ParentCommit(ctx, child)
	if err != nil || !ok || parent != root {
		t.Fatalf("ParentCommit(child) = %q ok=%v err=%v", parent, ok, err)
	}
	if _, ok, err = repo.ParentCommit(ctx, root); err != nil || ok {
		t.Fatalf("a root commit has no parent: ok=%v err=%v", ok, err)
	}
	shallow, err := repo.IsShallow(ctx)
	if err != nil || shallow {
		t.Fatalf("IsShallow = %v, %v", shallow, err)
	}

	clone := filepath.Join(t.TempDir(), "shallow")
	src := filepath.ToSlash(dir)
	runGit(t, filepath.Dir(clone), "clone", "-q", "--depth", "1", "file:///"+src, clone)
	shallowRepo, err := Open(ctx, clone, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open(clone): %v", err)
	}
	shallow, err = shallowRepo.IsShallow(ctx)
	if err != nil || !shallow {
		t.Fatalf("IsShallow(clone) = %v, %v", shallow, err)
	}
}

func TestCreateTagAtPinsTheCommit(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	first := commitFile(t, dir, "a.txt", "alpha\n", "chore: first")
	second := commitFile(t, dir, "b.txt", "beta\n", "chore: second")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.CreateTagAt(ctx, "v1.0.0", "Release v1.0.0", first); err != nil {
		t.Fatalf("CreateTagAt: %v", err)
	}
	points, err := repo.TagPointsAt(ctx, "v1.0.0", first)
	if err != nil || !points {
		t.Fatalf("the tag must point at the requested commit: %v %v", points, err)
	}
	points, err = repo.TagPointsAt(ctx, "v1.0.0", second)
	if err != nil {
		t.Fatal(err)
	}
	if points {
		t.Fatal("the tag must not point at HEAD")
	}
}

func TestWorktreeSnapshotSeparation(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	commitFile(t, dir, "content.txt", "alpha\n", "chore: first")
	second := commitFile(t, dir, "content.txt", "beta\n", "chore: second")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first := strings.TrimSpace(string(mustOutput(t, dir, "rev-parse", second+"^")))
	parent := filepath.Join(t.TempDir(), "snapshot")
	target := filepath.Join(parent, "src")
	if err := repo.AddWorktree(ctx, target, first); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(target, "content.txt"))
	if err != nil {
		t.Fatalf("the snapshot must be checked out: %v", err)
	}
	if strings.TrimSpace(string(content)) != "alpha" {
		t.Fatalf("snapshot content = %q, want the pinned commit", content)
	}
	if err := repo.RemoveWorktree(ctx, target); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the snapshot directory must be gone, err=%v", err)
	}
}

// TestRemoteTagCommitReadsTheRemote pins the three outcomes the tag step needs:
// the commit of an annotated tag, an absent tag, and a failed lookup that must
// not read as "no tag".
func TestRemoteTagCommitReadsTheRemote(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	pinned := commitFile(t, dir, "a.txt", "alpha\n", "chore: first")
	head := commitFile(t, dir, "b.txt", "beta\n", "chore: second")

	bare := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, dir, "init", "--bare", "-q", bare)
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	runGit(t, dir, "tag", "-a", "v1.0.0", "-m", "Release v1.0.0", pinned)
	runGit(t, dir, "push", "-q", "origin", "refs/tags/v1.0.0")

	ctx := context.Background()
	repo, err := Open(ctx, dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	commit, found, err := repo.RemoteTagCommit(ctx, "origin", "v1.0.0")
	if err != nil || !found {
		t.Fatalf("RemoteTagCommit(v1.0.0) = %q found=%v err=%v", commit, found, err)
	}
	if commit != pinned {
		t.Fatalf("the annotated tag must peel to %s, got %s (head is %s)", pinned, commit, head)
	}
	commit, found, err = repo.RemoteTagCommit(ctx, "origin", "v9.9.9")
	if err != nil || found || commit != "" {
		t.Fatalf("an absent tag must report found=false: %q found=%v err=%v", commit, found, err)
	}
	if _, _, err = repo.RemoteTagCommit(ctx, filepath.Join(t.TempDir(), "missing.git"), "v1.0.0"); err == nil {
		t.Fatal("an unreachable remote must be an error, not an absent tag")
	}
}
