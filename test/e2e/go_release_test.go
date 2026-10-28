package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newGoRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "e2e@example.com")
	git(t, dir, "config", "user.name", "e2e")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte("package demo\n\nconst Name = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func gitFail(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git %v unexpectedly succeeded:\n%s", args, out)
	}
	return string(out)
}

func gitText(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// runStrict fails on any command error, unlike run which tolerates expected
// "error:" output.
func runStrict(t *testing.T, bin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--cwd", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("qegg %v: %v\n%s", args, err, out)
	}
	return string(out)
}

type goPlanReport struct {
	Phase   string
	Version struct {
		Current string
		Next    string
	}
	Tag string
}

func readGoPlan(t *testing.T, bin, dir string) goPlanReport {
	t.Helper()
	out, _ := run(t, bin, dir, "plan", "--json")
	var plan goPlanReport
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("plan json: %v\n%s", err, out)
	}
	return plan
}

// TestGoPreCommitHookBlocksFailingTests pins the commit-time gate: the
// generated pre-commit hook runs [verify].commands and refuses the commit when
// they fail.
func TestGoPreCommitHookBlocksFailingTests(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "go", "--no-workflows")

	failing := "package demo\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { t.Fatal(\"boom\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "demo_test.go"), []byte(failing), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")

	before := gitText(t, dir, "rev-list", "--count", "HEAD")
	cmd := exec.Command(bin, "--cwd", dir, "commit", "--no-input", "-m", "feat: break the build", "--packages", "example.com/demo:patch")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("qegg commit must fail when the pre-commit verification fails:\n%s", out)
	}
	if after := gitText(t, dir, "rev-list", "--count", "HEAD"); after != before {
		t.Fatalf("the hook must block the commit: %s -> %s", before, after)
	}

	if err := os.Remove(filepath.Join(dir, "demo_test.go")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: fix the build")
	if after := gitText(t, dir, "rev-list", "--count", "HEAD"); after == before {
		t.Fatal("the commit should succeed once the verification passes")
	}
}

// TestGoReleaseAgainstLocalRemote exercises the whole Go release pipeline
// without GitHub or a package registry: a bare repository plays origin, so the
// tag and push steps are covered before the first real release exists.
func TestGoReleaseAgainstLocalRemote(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)

	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", remote)
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-q", "origin", "HEAD")

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "go",
		"--no-hook", "--no-workflows", "--no-verify-cmd")
	git(t, dir, "add", "-A")
	runStrict(t, bin, dir, "commit", "--no-input", "-m", "feat(go): support go modules",
		"--packages", "example.com/demo:minor")

	plan := readGoPlan(t, bin, dir)
	if plan.Phase != "version" || plan.Version.Current != "0.0.0" || plan.Version.Next != "0.1.0" || plan.Tag != "v0.1.0" {
		t.Fatalf("unexpected first plan: %+v", plan)
	}

	runStrict(t, bin, dir, "release", "--yes", "--no-publish", "--no-github-release")

	if tags := gitText(t, dir, "tag", "--list"); !strings.Contains(tags, "v0.1.0") {
		t.Fatalf("local tag v0.1.0 missing: %q", tags)
	}
	if tags := gitText(t, dir, "--git-dir", remote, "tag", "--list"); !strings.Contains(tags, "v0.1.0") {
		t.Fatalf("remote tag v0.1.0 missing: %q", tags)
	}
	changelog, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("changelog: %v", err)
	}
	if !strings.Contains(string(changelog), "## v0.1.0") {
		t.Fatalf("unexpected changelog:\n%s", changelog)
	}
	marker, err := os.ReadFile(filepath.Join(dir, ".quailegg", "release.json"))
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if !strings.Contains(string(marker), "\"version\": \"0.1.0\"") {
		t.Fatalf("unexpected marker:\n%s", marker)
	}
	if again := readGoPlan(t, bin, dir); again.Phase != "none" {
		t.Fatalf("expected a no-op plan after the release, got %+v", again)
	}

	runStrict(t, bin, dir, "commit", "--no-input", "-m", "fix(go): patch something",
		"--packages", "example.com/demo:patch")
	next := readGoPlan(t, bin, dir)
	if next.Phase != "version" || next.Version.Current != "0.1.0" || next.Version.Next != "0.1.1" {
		t.Fatalf("unexpected second plan: %+v", next)
	}

	runStrict(t, bin, dir, "release", "--yes", "--no-publish", "--no-github-release")
	tags := gitText(t, dir, "--git-dir", remote, "tag", "--list")
	for _, want := range []string{"v0.1.0", "v0.1.1"} {
		if !strings.Contains(tags, want) {
			t.Fatalf("remote tags %q are missing %s", tags, want)
		}
	}
}
