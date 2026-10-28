package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func buildBinary(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	bin := filepath.Join(t.TempDir(), "qegg")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/qegg")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func run(t *testing.T, bin, dir string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--cwd", dir}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("qegg %v: %v\n%s%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "e2e@example.com")
	git(t, dir, "config", "user.name", "e2e")
	pkg := "{\n  \"name\": \"e2e-pkg\",\n  \"version\": \"1.0.0\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func TestInitCheckCommitPlan(t *testing.T) {
	bin := buildBinary(t)
	dir := newRepo(t)

	if _, stderr := run(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "npm"); stderr != "" && strings.Contains(stderr, "error:") {
		t.Fatalf("init failed: %s", stderr)
	}
	for _, expected := range []string{
		".quailegg.toml",
		".changes/README.md",
		".github/workflows/qegg-release.yml",
		".github/workflows/qegg-pr.yml",
		".github/workflows/qegg-status.yml",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(expected))); err != nil {
			t.Fatalf("missing %s: %v", expected, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); err != nil {
		t.Fatalf("missing hook: %v", err)
	}

	checkOut, _ := run(t, bin, dir, "check", "--json")
	var report struct {
		OK        bool   `json:"ok"`
		Mode      string `json:"mode"`
		Ecosystem string `json:"ecosystem"`
	}
	if err := json.Unmarshal([]byte(checkOut), &report); err != nil {
		t.Fatalf("check json: %v\n%s", err, checkOut)
	}
	if !report.OK || report.Ecosystem != "npm" || report.Mode != "commit" {
		t.Fatalf("unexpected check report: %+v", report)
	}

	run(t, bin, dir, "commit", "--no-input", "-m", "feat(cache): support cache ttl", "--packages", "e2e-pkg:minor", "--no-commit")
	records, err := os.ReadDir(filepath.Join(dir, ".changes"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range records {
		if strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "README.md" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one record, got %d", count)
	}

	planOut, _ := run(t, bin, dir, "plan", "--json")
	var plan struct {
		Phase   string `json:"phase"`
		Version struct {
			Current string `json:"current"`
			Next    string `json:"next"`
		} `json:"version"`
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal([]byte(planOut), &plan); err != nil {
		t.Fatalf("plan json: %v\n%s", err, planOut)
	}
	if plan.Phase != "version" || plan.Version.Next != "1.1.0" || plan.Tag != "v1.1.0" {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	changelogOut, _ := run(t, bin, dir, "changelog", "--dry-run")
	if !strings.Contains(changelogOut, "## v1.1.0") {
		t.Fatalf("unexpected changelog preview:\n%s", changelogOut)
	}
}

func TestHookScriptIsInstalled(t *testing.T) {
	bin := buildBinary(t)
	dir := newRepo(t)
	run(t, bin, dir, "init", "--no-input", "--mode", "pr", "--registries", "npm")
	data, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "qegg hook pre-commit") {
		t.Fatalf("unexpected hook script:\n%s", data)
	}
}

// isolateUserConfig points the CLI's per-user config directory at a temporary
// location and returns the config file path this platform should use.
func isolateUserConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
		return filepath.Join(dir, "qegg", "config.toml")
	case "darwin":
		t.Setenv("HOME", dir)
		return filepath.Join(dir, "Library", "Application Support", "qegg", "config.toml")
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
		return filepath.Join(dir, "qegg", "config.toml")
	}
}

// TestUserLanguageBeatsLegacyProjectConfig pins the reported bug: a repository
// scaffolded by an older qegg pins language = "en" in .quailegg.toml, but the
// per-user preference (qegg lang zh) must still win.
func TestUserLanguageBeatsLegacyProjectConfig(t *testing.T) {
	bin := buildBinary(t)
	dir := newRepo(t)
	run(t, bin, dir, "init", "--no-input", "--mode", "pr", "--registries", "npm")

	cfgPath := filepath.Join(dir, ".quailegg.toml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(data), "schema_version = 1\n", "schema_version = 1\nlanguage = \"en\"\n", 1)
	if patched == string(data) {
		t.Fatalf("unexpected generated config:\n%s", data)
	}
	if err := os.WriteFile(cfgPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	// Isolate after building and preparing the repository so go and git keep
	// their usual environment.
	userConfig := isolateUserConfig(t)
	t.Setenv("QEGG_LANG", "")

	run(t, bin, dir, "lang", "zh")
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatalf("language preference was not saved to %s: %v", userConfig, err)
	}

	out, stderr := run(t, bin, dir, "check")
	if combined := out + stderr; !strings.Contains(combined, "生态") {
		t.Fatalf("expected Chinese output despite the legacy project language:\nstdout:\n%s\nstderr:\n%s", out, stderr)
	}

	out, stderr = run(t, bin, dir, "--lang", "en", "check")
	if combined := out + stderr; strings.Contains(combined, "生态") {
		t.Fatalf("--lang en should still override the user preference:\nstdout:\n%s\nstderr:\n%s", out, stderr)
	}
}
