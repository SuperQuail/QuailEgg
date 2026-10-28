// Package ecosystemtest provides the shared fixtures used by the ecosystem
// adapter tests.
package ecosystemtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fixture copies testdata/<name> into a temporary directory and returns it, so
// a test can rewrite manifests without touching the repository.
func Fixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(testdataRoot(), name)
	dst := filepath.Join(t.TempDir(), name)
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dst
}

func testdataRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate the testdata directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
