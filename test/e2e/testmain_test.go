package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// e2eBinary is the CLI every end-to-end test runs. It is built once, because
// compiling it per test was the largest cost of this package.
var e2eBinary string

// TestMain builds the CLI once and keeps the environment a real runner would set
// out of the tests. Networking is switched off per test through offlineGitHub.
func TestMain(m *testing.M) {
	// The release identity and the author lookup read these; a runner that sets
	// them must not change what the tests observe. GITHUB_API_URL is deliberately
	// not set here: tests that must stay off the network call offlineGitHub, so a
	// test that expects the gh CLI keeps the real API base.
	os.Setenv("GITHUB_REPOSITORY", "")
	os.Setenv("GITHUB_SERVER_URL", "")
	os.Setenv("CI", "")
	os.Setenv("GITHUB_ACTIONS", "")
	dir, err := os.MkdirTemp("", "qegg-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	if _, err := exec.LookPath("go"); err == nil {
		bin := filepath.Join(dir, "qegg")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		root, rootErr := filepath.Abs(filepath.Join("..", ".."))
		if rootErr != nil {
			fmt.Fprintln(os.Stderr, rootErr)
			os.Exit(1)
		}
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/qegg")
		cmd.Dir = root
		if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
			fmt.Fprintf(os.Stderr, "go build: %v\n%s", buildErr, out)
			os.Exit(1)
		}
		e2eBinary = bin
	}
	os.Exit(m.Run())
}

// offlineGitHub points the optional GitHub lookups at a closed local port, which
// the author lookup treats as offline, so plans stay deterministic and hermetic.
// It is per test on purpose: the gh CLI path is only taken when the API base is
// github.com, so a test that expects the gh stub calls onlineGitHub instead.
func offlineGitHub(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_API_URL", "http://127.0.0.1:1")
}

// onlineGitHub restores the real API base for a test that expects the gh CLI.
func onlineGitHub(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_API_URL", "")
}
