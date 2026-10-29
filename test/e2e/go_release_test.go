package e2e

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newGoRepo(t *testing.T) string {
	t.Helper()
	sanitizeGitEnv()
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
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	mainSource := "package main\n\nimport \"fmt\"\n\nvar version = \"dev\"\n\nfunc main() { fmt.Println(version) }\n"
	if err := os.WriteFile(filepath.Join(dir, "cmd", "demo", "main.go"), []byte(mainSource), 0o644); err != nil {
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
	Tag    string
	Assets *struct {
		Name    string
		Main    string
		Targets []struct {
			OS   string
			Arch string
		}
	}
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

func appendConfig(t *testing.T, dir, text string) {
	t.Helper()
	path := filepath.Join(dir, ".quailegg.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := os.WriteFile(path, append(data, []byte(text)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hostAssetTarget pins the release to the platform running the test, so the
// produced binary can be executed to verify the injected version.
func hostAssetTarget() string {
	return "\n[release.assets]\ntargets = [\"" + runtime.GOOS + "/" + runtime.GOARCH + "\"]\n"
}

func findArchive(t *testing.T, dir, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("assets dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip")) {
			return filepath.Join(dir, name)
		}
	}
	t.Fatalf("archive with prefix %q is missing from %s", prefix, dir)
	return ""
}

func extractArchive(t *testing.T, archive, dst string) string {
	t.Helper()
	if strings.HasSuffix(archive, ".zip") {
		reader, err := zip.OpenReader(archive)
		if err != nil {
			t.Fatalf("open zip: %v", err)
		}
		defer reader.Close()
		for _, file := range reader.File {
			target := filepath.Join(dst, filepath.Base(file.Name))
			src, err := file.Open()
			if err != nil {
				t.Fatalf("zip entry: %v", err)
			}
			data, err := io.ReadAll(src)
			src.Close()
			if err != nil {
				t.Fatalf("read zip entry: %v", err)
			}
			writeExecutable(t, target, data)
			return target
		}
		t.Fatal("zip archive is empty")
	}

	file, err := os.Open(archive)
	if err != nil {
		t.Fatalf("open tar: %v", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		target := filepath.Join(dst, filepath.Base(header.Name))
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		writeExecutable(t, target, data)
		return target
	}
	t.Fatal("tar archive is empty")
	return ""
}

func writeExecutable(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod asset: %v", err)
	}
}

func runBinary(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

// TestGoPreCommitHookBlocksFailingTests pins the commit-time gate: the
// generated pre-commit hook runs [verify].commands and refuses the commit when
// they fail.
func TestGoPreCommitHookBlocksFailingTests(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	addOrigin(t, dir)

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
// tag, push and release-asset steps are covered before the first real release.
func TestGoReleaseAgainstLocalRemote(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)

	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", remote)
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-q", "origin", "HEAD")

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "go",
		"--no-hook", "--no-workflows", "--no-verify-cmd")
	appendConfig(t, dir, hostAssetTarget())
	git(t, dir, "add", "-A")
	runStrict(t, bin, dir, "commit", "--no-input", "-m", "feat(go): support go modules",
		"--packages", "example.com/demo:minor")

	plan := readGoPlan(t, bin, dir)
	if plan.Phase != "version" || plan.Version.Current != "0.0.0" || plan.Version.Next != "0.1.0" || plan.Tag != "v0.1.0" {
		t.Fatalf("unexpected first plan: %+v", plan)
	}
	if plan.Assets == nil || plan.Assets.Name != "demo" || plan.Assets.Main != "cmd/demo" {
		t.Fatalf("plan is missing the release assets: %+v", plan.Assets)
	}
	if len(plan.Assets.Targets) != 1 || plan.Assets.Targets[0].OS != runtime.GOOS || plan.Assets.Targets[0].Arch != runtime.GOARCH {
		t.Fatalf("unexpected asset targets: %+v", plan.Assets.Targets)
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

	assetsDir := filepath.Join(dir, ".quailegg", "assets", "v0.1.0")
	archive := findArchive(t, assetsDir, "demo_0.1.0_"+runtime.GOOS+"_"+runtime.GOARCH)
	checksums, err := os.ReadFile(filepath.Join(assetsDir, "checksums.txt"))
	if err != nil {
		t.Fatalf("checksums: %v", err)
	}
	if !strings.Contains(string(checksums), filepath.Base(archive)) {
		t.Fatalf("checksums do not cover %s:\n%s", archive, checksums)
	}
	binary := extractArchive(t, archive, t.TempDir())
	if got := runBinary(t, binary); got != "0.1.0" {
		t.Fatalf("binary version = %q, want 0.1.0", got)
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
	secondArchive := findArchive(t, filepath.Join(dir, ".quailegg", "assets", "v0.1.1"), "demo_0.1.1_"+runtime.GOOS+"_"+runtime.GOARCH)
	if !strings.Contains(filepath.Base(secondArchive), "demo_0.1.1") {
		t.Fatalf("unexpected second archive: %s", secondArchive)
	}
}
