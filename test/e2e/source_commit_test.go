package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeSourceConfig scaffolds a PR-mode Go release that builds host assets.
func writeSourceConfig(t *testing.T, dir string) {
	t.Helper()
	content := "schema_version = 1\n\n" +
		"[project]\ndefault_branch = \"main\"\n\n" +
		"[git]\ntag_prefix = \"v\"\nremote = \"origin\"\n\n" +
		"[release]\nmode = \"pr\"\ngithub_release = false\n\n" +
		"[changes]\ndir = \".changes\"\nscope = [\"cmd\", \"internal\", \"test\"]\n\n" +
		"[verify]\ncommands = []\ntimeout = \"5m\"\nfail_fast = true\n\n" +
		"[publish]\nregistries = [\"go\"]\n\n" +
		"[release.assets]\ntargets = [\"" + runtime.GOOS + "/" + runtime.GOARCH + "\"]\n\n" +
		"[packages.\"example.com/demo\"]\npath = \".\"\nresolver = \"go\"\npublish = false\n"
	if err := os.WriteFile(filepath.Join(dir, ".quailegg.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ignored := ".quailegg/state/\n.quailegg/lock\n.quailegg/assets/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignored), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeBanner makes the demo binary print the banner it was built from, so the
// source of an attachment can be read back from the binary itself.
func writeBanner(t *testing.T, dir, banner string) {
	t.Helper()
	main := "package main\n\nimport \"fmt\"\n\nconst banner = \"" + banner + "\"\n\nfunc main() { fmt.Println(banner) }\n"
	if err := os.WriteFile(filepath.Join(dir, "cmd", "demo", "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runFail runs qegg and returns its combined output, requiring a failure.
func runFail(t *testing.T, bin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--cwd", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("qegg %v unexpectedly succeeded:\n%s", args, out)
	}
	return string(out)
}

// ensureGitIdentity gives a repository a local identity. CI runners have no
// global git user, and a clone does not inherit the local configuration of the
// repository it was cloned from, so anything that tags would otherwise fail.
func ensureGitIdentity(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "config", "user.email", "e2e@example.com")
	git(t, dir, "config", "user.name", "e2e")
}

// remoteTag returns the commit the bare remote's tag points at, empty when the
// remote has no such tag.
func remoteTag(t *testing.T, remote, tag string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-list", "-n", "1", tag).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// prepareReleasePR drives the version phase, merges the release PR the way
// GitHub squashes it, moves main on to different content and returns the
// release commit, the new head and the bare origin.
func prepareReleasePR(t *testing.T) (dir, origin, release, head string) {
	t.Helper()
	binSrc := newGoRepo(t)
	dir = binSrc
	git(t, dir, "branch", "-M", "main")
	writeSourceConfig(t, dir)
	writeBanner(t, dir, "vA")
	writeRecord(t, dir, "add-feature", "Add a feature.")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: add a feature")

	origin = filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", origin)
	// A clone checks out the remote HEAD, so point it at main explicitly.
	git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, dir, "remote", "add", "origin", origin)
	git(t, dir, "push", "-q", "origin", "main")
	installFakeGH(t)

	bin := buildBinary(t)
	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")

	// The version phase leaves the checkout on the release branch; the pull
	// request is squash-merged into main, so main gets a new commit with the
	// release tree and the release-branch commit never lands on it.
	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "fetch", "-q", "origin", "release")
	git(t, dir, "merge", "--squash", "FETCH_HEAD")
	git(t, dir, "commit", "-qm", "chore(release): bump versions (#7)")
	release = gitText(t, dir, "rev-parse", "HEAD")

	writeBanner(t, dir, "vB")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: move on")
	head = gitText(t, dir, "rev-parse", "HEAD")
	git(t, dir, "push", "-q", "origin", "main")
	return dir, origin, release, head
}

// TestReleaseUsesThePinnedSourceCommit is the A/B case from the task list: the
// release is prepared on commit A, main moves to B, and the resume from B must
// still build and tag the release of A. The upload half runs against a stub
// registry command in TestPublishRunsInsideTheReleaseSnapshot.
func TestReleaseUsesThePinnedSourceCommit(t *testing.T) {
	bin := buildBinary(t)
	dir, origin, release, head := prepareReleasePR(t)

	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")

	if got := gitText(t, dir, "rev-parse", "v0.1.0^{commit}"); got != release {
		t.Fatalf("the local tag points at %s, want the release commit %s (head is %s)", got, release, head)
	}
	if got := remoteTag(t, origin, "v0.1.0"); got != release {
		t.Fatalf("the remote tag points at %s, want %s", got, release)
	}
	if got := remoteHead(t, dir, "main"); got != head {
		t.Fatalf("main moved during the publish phase: %s -> %s", head, got)
	}
	if got := remoteHead(t, dir, "release"); got != release {
		t.Fatalf("the release branch must be flattened onto the published commit: got %s want %s", got, release)
	}

	// The package content at the tag is the release commit, not the moved tree.
	if got := gitText(t, dir, "show", "v0.1.0:cmd/demo/main.go"); !strings.Contains(got, "vA") {
		t.Fatalf("the tagged source is not the release commit:\n%s", got)
	}
	if got := gitText(t, dir, "show", "HEAD:cmd/demo/main.go"); !strings.Contains(got, "vB") {
		t.Fatalf("the working tree must keep its own content:\n%s", got)
	}

	// The attachment was built from A as well.
	assetsDir := filepath.Join(dir, ".quailegg", "assets", "v0.1.0")
	archive := findArchive(t, assetsDir, "demo_0.1.0_")
	binary := extractArchive(t, archive, t.TempDir())
	if got := runBinary(t, binary); got != "vA" {
		t.Fatalf("the release asset was built from %q, want the release commit content", got)
	}
	checksums, err := os.ReadFile(filepath.Join(assetsDir, "checksums.txt"))
	if err != nil {
		t.Fatalf("checksums: %v", err)
	}
	if !strings.Contains(string(checksums), filepath.Base(archive)) {
		t.Fatalf("checksums do not cover the archive:\n%s", checksums)
	}
}

// TestReleaseStopsWhenTheSourceIsUnavailable covers the second legacy outcome:
// a shallow clone cannot name the release snapshot, so the run stops before the
// first external write instead of building from whatever is checked out.
func TestReleaseStopsWhenTheSourceIsUnavailable(t *testing.T) {
	bin := buildBinary(t)
	dir, origin, release, _ := prepareReleasePR(t)

	shallow := filepath.Join(t.TempDir(), "shallow")
	git(t, dir, "clone", "-q", "--depth", "1", "file:///"+filepath.ToSlash(origin), shallow)
	ensureGitIdentity(t, shallow)

	out := runFail(t, bin, shallow, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")
	if !strings.Contains(out, "shallow clone") {
		t.Fatalf("the failure must name the shallow history:\n%s", out)
	}
	// An exit status alone proves nothing: a missing git identity exits 1 as
	// well, and that is not the refusal this test is about.
	if strings.Contains(out, "identity") || strings.Contains(out, "Committer") || strings.Contains(out, "Author") {
		t.Fatalf("the refusal must come from the source lookup, not from git:\n%s", out)
	}
	if tags := gitText(t, shallow, "tag", "--list"); tags != "" {
		t.Fatalf("a stopped release must not create a tag: %q", tags)
	}
	if tags := gitText(t, dir, "--git-dir", origin, "tag", "--list"); tags != "" {
		t.Fatalf("a stopped release must not push a tag: %q", tags)
	}
	if _, err := os.Stat(filepath.Join(shallow, ".quailegg", "assets", "v0.1.0")); !os.IsNotExist(err) {
		t.Fatalf("a stopped release must not build assets: %v", err)
	}
	if got := remoteTag(t, origin, "v0.1.0"); got != "" {
		t.Fatalf("the remote must not have the release tag: %q", got)
	}
	if release == "" {
		t.Fatal("the fixture must produce a release commit")
	}

	// The same run succeeds once the history is there.
	git(t, shallow, "fetch", "-q", "--unshallow", "origin")
	runStrict(t, bin, shallow, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")
	if got := remoteTag(t, origin, "v0.1.0"); got != release {
		t.Fatalf("the remote tag points at %s, want %s", got, release)
	}
}

// writeAssetProvenance writes one attachment and the manifest that names the
// commit it was built from.
func writeAssetProvenance(t *testing.T, dir, commit string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "demo_0.1.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	payload := []byte("payload")
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	manifest := map[string]any{
		"schema_version": 1,
		"source_commit":  commit,
		"sha256":         map[string]string{name: hex.EncodeToString(sum[:])},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qegg-assets.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReleaseRefusesAssetsFromAnotherCommit pins the provenance rule: an
// attachment that names another commit must not be uploaded to this release.
func TestReleaseRefusesAssetsFromAnotherCommit(t *testing.T) {
	bin := buildBinary(t)
	dir, _, release, head := prepareReleasePR(t)

	assetsDir := filepath.Join(t.TempDir(), "dist")
	writeAssetProvenance(t, assetsDir, head)
	out := runFail(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release", "--assets-dir", assetsDir)
	if !strings.Contains(out, "built from") {
		t.Fatalf("the failure must name the asset source commit:\n%s", out)
	}
	if tags := gitText(t, dir, "tag", "--list"); tags != "" {
		t.Fatalf("a refused release must not create a tag: %q", tags)
	}

	// Without a manifest the attachment cannot prove where it came from either.
	if err := os.Remove(filepath.Join(assetsDir, "qegg-assets.json")); err != nil {
		t.Fatal(err)
	}
	out = runFail(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release", "--assets-dir", assetsDir)
	if !strings.Contains(out, "qegg-assets.json") {
		t.Fatalf("the failure must name the missing manifest:\n%s", out)
	}

	// The same attachment is accepted once it names the release commit.
	writeAssetProvenance(t, assetsDir, release)
	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release", "--assets-dir", assetsDir)
	if got := gitText(t, dir, "rev-parse", "v0.1.0^{commit}"); got != release {
		t.Fatalf("the tag points at %s, want %s", got, release)
	}
}
