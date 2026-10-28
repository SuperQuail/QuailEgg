package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func TestCommitNoVerifySkipsHooks(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := Open(context.Background(), dir, execx.OSRunner{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := repo.Add(ctx, "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, "chore: blocked"); err == nil {
		t.Fatal("expected the pre-commit hook to block the commit")
	}
	sha, err := repo.CommitNoVerify(ctx, "chore: allowed")
	if err != nil {
		t.Fatalf("CommitNoVerify: %v", err)
	}
	if sha == "" {
		t.Fatal("CommitNoVerify returned an empty sha")
	}
	log, err := repo.output(ctx, "log", "-1", "--pretty=%s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "chore: allowed") {
		t.Fatalf("unexpected commit message %q", log)
	}
}
