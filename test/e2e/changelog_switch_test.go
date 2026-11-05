package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoReleaseWithoutChangelog pins the opt-out end to end: qegg init writes
// the switch, and a release then tags the version without writing CHANGELOG.md.
func TestGoReleaseWithoutChangelog(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)

	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", remote)
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-q", "origin", "HEAD")

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "go",
		"--no-hook", "--no-workflows", "--no-verify-cmd", "--no-changelog")
	configText, err := os.ReadFile(filepath.Join(dir, ".quailegg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configText), "changelog = false") {
		t.Fatalf("init did not write the changelog switch:\n%s", configText)
	}
	git(t, dir, "add", "-A")
	runStrict(t, bin, dir, "commit", "--no-input", "-m", "feat(go): add a feature",
		"--packages", "example.com/demo:minor")

	runStrict(t, bin, dir, "release", "--yes", "--no-publish", "--no-github-release")

	if tags := gitText(t, dir, "tag", "--list"); !strings.Contains(tags, "v0.1.0") {
		t.Fatalf("local tag v0.1.0 missing: %q", tags)
	}
	if _, err := os.Stat(filepath.Join(dir, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("a release with the changelog off must not write CHANGELOG.md (err=%v)", err)
	}
	committed := gitText(t, dir, "show", "--name-only", "--format=", "HEAD")
	if strings.Contains(committed, "CHANGELOG.md") {
		t.Fatalf("the release commit staged CHANGELOG.md:\n%s", committed)
	}
}
