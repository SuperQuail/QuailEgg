package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestResumePushesTheMissingTagAfterAFailedPush drives the real CLI: the release
// uploads nothing (a Go module is released by its tag), creates the tag locally
// and fails to push it. A resume must push only that tag and must not repeat
// anything else. On the base revision resume found nothing to do and left the
// tag unpublished, which is what this test pins.
func TestResumePushesTheMissingTagAfterAFailedPush(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)

	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", remote)
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "go",
		"--no-hook", "--no-workflows", "--no-verify-cmd")
	git(t, dir, "add", "-A")
	runStrict(t, bin, dir, "commit", "--no-input", "-m", "feat(go): support go modules",
		"--packages", "example.com/demo:minor")

	allowPushes := rejectTagPushHook(t, remote)
	first := exec.Command(bin, "--cwd", dir, "release", "--yes", "--no-github-release", "--no-assets")
	if out, err := first.CombinedOutput(); err == nil {
		t.Fatalf("a refused tag push must fail the release:\n%s", out)
	}

	tag := "v0.1.0"
	// The annotated tag object is what a resume has to push unchanged, while the
	// commit it peels to is the release commit.
	local := strings.TrimSpace(gitText(t, dir, "rev-parse", tag))
	localCommit := strings.TrimSpace(gitText(t, dir, "rev-parse", tag+"^{}"))
	if local == "" || localCommit == "" {
		t.Fatal("the release did not create its tag")
	}
	if remoteTags := gitText(t, dir, "--git-dir", remote, "tag", "--list"); remoteTags != "" {
		t.Fatalf("the refused push must leave the remote without tags: %q", remoteTags)
	}
	if refs := gitText(t, dir, "ls-remote", remote, "refs/qegg/ledger/"+tag); !strings.Contains(refs, "refs/qegg/ledger/"+tag) {
		t.Fatalf("the publish record was not written before the uploads: %q", refs)
	}

	allowPushes()
	resumed := exec.Command(bin, "--cwd", dir, "resume")
	if out, err := resumed.CombinedOutput(); err != nil {
		t.Fatalf("resume: %v\n%s", err, out)
	}
	if got := gitText(t, dir, "--git-dir", remote, "tag", "--list"); !strings.Contains(got, tag) {
		t.Fatalf("resume did not push the missing tag: %q", got)
	}
	if got := gitText(t, dir, "rev-parse", tag); got != local {
		t.Fatalf("resume recreated the local tag: %s -> %s", local, got)
	}
	if pushed := gitText(t, dir, "ls-remote", remote, "refs/tags/"+tag); !strings.Contains(pushed, local) {
		t.Fatalf("the pushed tag is not the local tag object: %q, want %s", pushed, local)
	}
	if peeled := gitText(t, dir, "ls-remote", remote, "refs/tags/"+tag+"^{}"); !strings.Contains(peeled, localCommit) {
		t.Fatalf("the pushed tag points at %q, want the release commit %s", peeled, localCommit)
	}

	// Running the same resume again must not move a single remote reference.
	before := gitText(t, dir, "ls-remote", remote)
	again := exec.Command(bin, "--cwd", dir, "resume")
	if out, err := again.CombinedOutput(); err != nil {
		t.Fatalf("second resume: %v\n%s", err, out)
	}
	if after := gitText(t, dir, "ls-remote", remote); after != before {
		t.Fatalf("the second resume changed the remote:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
