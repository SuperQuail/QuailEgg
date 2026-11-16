package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newSharedReleaseRepo builds a repository that mixes a tag-versioned Go module
// at the root with a manifest-versioned npm package under one shared version.
func newSharedReleaseRepo(t *testing.T) string {
	t.Helper()
	sanitizeGitEnv()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "e2e@example.com")
	git(t, dir, "config", "user.name", "e2e")
	writeE2EFile(t, dir, "go.mod", "module example.com/demo\n\ngo 1.21\n")
	writeE2EFile(t, dir, "demo.go", "package demo\n\nconst Name = \"demo\"\n")
	writeE2EFile(t, dir, "frontend/package.json", "{\n  \"name\": \"frontend\",\n  \"version\": \"0.0.0\",\n  \"private\": true\n}\n")
	writeE2EFile(t, dir, ".changes/add-feature.md", "---\ntype: feat\npackages:\n  go@demo: minor\n  npm@frontend: minor\n---\n\nAdd a feature.\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: add a feature")
	git(t, dir, "branch", "-M", "main")
	return dir
}

// writeSharedReleaseConfig registers the Go module and the npm package under a
// shared version scope in pr mode.
func writeSharedReleaseConfig(t *testing.T, dir string) {
	t.Helper()
	content := `schema_version = 1

[project]
default_branch = "main"

[git]
tag_prefix = "v"
remote = "origin"

[release]
mode = "pr"
version_scope = "shared"
github_release = false

[changes]
dir = ".changes"
scope = ["cmd", "internal", "test"]

[verify]
commands = []
timeout = "5m"
fail_fast = true

[publish]
registries = ["go"]

[packages."go@demo"]
name = "example.com/demo"
path = "."
resolver = "go"
publish = false

[packages."npm@frontend"]
name = "frontend"
path = "frontend"
resolver = "npm"
publish = false
`
	if err := os.WriteFile(filepath.Join(dir, ".quailegg.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sharedPlanReport is the part of qegg plan --json this test asserts on.
type sharedPlanReport struct {
	Phase   string `json:"phase"`
	Version struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	} `json:"version"`
	Tag string `json:"tag"`
}

// readSharedPlan runs qegg plan and decodes its machine readable form.
func readSharedPlan(t *testing.T, bin, dir string) sharedPlanReport {
	t.Helper()
	out := runStrict(t, bin, dir, "plan", "--json")
	var plan sharedPlanReport
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("plan json: %v\n%s", err, out)
	}
	return plan
}

// TestSharedReleasePublishesTagVersionedPackage drives the two-phase pr release
// end to end. The version phase bumps the npm manifest and leaves Go on its
// previous tag; the publish phase has to accept that through the marker's
// pending version instead of reporting a shared version mismatch.
func TestSharedReleasePublishesTagVersionedPackage(t *testing.T) {
	bin := buildBinary(t)
	dir := newSharedReleaseRepo(t)
	writeSharedReleaseConfig(t, dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "chore: configure qegg")

	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", origin)
	git(t, dir, "remote", "add", "origin", origin)
	git(t, dir, "push", "-q", "origin", "main")
	installFakeGH(t)

	plan := readSharedPlan(t, bin, dir)
	if plan.Phase != "version" || plan.Version.Next != "0.1.0" || plan.Tag != "v0.1.0" {
		t.Fatalf("first plan = %+v, want phase version next 0.1.0 tag v0.1.0", plan)
	}

	runStrict(t, bin, dir, "check")
	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")

	if branch := gitText(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "release" {
		t.Fatalf("release phase left the branch at %q, want release", branch)
	}
	// The release branch carries the bumped npm manifest and the marker while Go
	// still has no tag, which is exactly the state the check has to accept.
	runStrict(t, bin, dir, "check")

	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "fetch", "-q", "origin", "release")
	git(t, dir, "merge", "--squash", "FETCH_HEAD")
	git(t, dir, "commit", "-qm", "chore(release): bump versions")
	git(t, dir, "push", "-q", "origin", "main")

	publish := readSharedPlan(t, bin, dir)
	if publish.Phase != "publish" || publish.Version.Next != "0.1.0" || publish.Tag != "v0.1.0" {
		t.Fatalf("publish plan = %+v, want phase publish next 0.1.0 tag v0.1.0", publish)
	}

	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")
	if tag := gitText(t, dir, "tag", "--list", "v0.1.0"); tag != "v0.1.0" {
		t.Fatalf("local tag = %q, want v0.1.0", tag)
	}
	if tag := gitText(t, dir, "--git-dir", origin, "tag", "--list", "v0.1.0"); tag != "v0.1.0" {
		t.Fatalf("remote tag = %q, want v0.1.0", tag)
	}
	tagged := gitText(t, dir, "show", "v0.1.0:frontend/package.json")
	if !strings.Contains(tagged, "0.1.0") {
		t.Fatalf("the tagged npm manifest does not carry 0.1.0:\n%s", tagged)
	}
}
