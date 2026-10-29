package changes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnforceIgnoresManagedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	ignore := []string{
		".quailegg.toml",
		".changes/README.md",
		".github/workflows/qegg-release.yml",
		".github/workflows/qegg-pr.yml",
		".github/workflows/qegg-status.yml",
		".git/hooks/pre-commit",
	}
	managed := []string{
		".quailegg.toml",
		".changes/README.md",
		".github/workflows/qegg-release.yml",
	}

	if err := Enforce(Staged{Root: root, Dir: ".changes", Ignore: ignore, Files: managed}); err != nil {
		t.Fatalf("qegg generated files required a record: %v", err)
	}

	files := append(append([]string{}, managed...), "src/cache.js")
	if err := Enforce(Staged{Root: root, Dir: ".changes", Ignore: ignore, Files: files}); err == nil {
		t.Fatal("a source change without a record should be rejected")
	}
}

func TestTouchesScope(t *testing.T) {
	ignore := []string{".quailegg.toml", ".changes/README.md"}
	if !TouchesScope([]string{"cmd/main.go"}, ".changes", []string{"cmd"}, ignore) {
		t.Fatal("a scoped file must count as a scope change")
	}
	if TouchesScope([]string{".changes/x.md", ".quailegg.toml"}, ".changes", []string{"cmd"}, ignore) {
		t.Fatal("records and managed files must not count as scope changes")
	}
}
