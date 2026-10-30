package assets

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

type recordedCall struct {
	Dir  string
	Env  []string
	Args []string
}

type fakeEnvRunner struct {
	calls []recordedCall
	err   error
}

func (f *fakeEnvRunner) Run(ctx context.Context, dir, name string, args ...string) (execx.Result, error) {
	return f.RunEnv(ctx, dir, nil, name, args...)
}

func (f *fakeEnvRunner) RunEnv(_ context.Context, dir string, env []string, name string, args ...string) (execx.Result, error) {
	f.calls = append(f.calls, recordedCall{Dir: dir, Env: env, Args: append([]string{name}, args...)})
	if f.err != nil {
		return execx.Result{}, f.err
	}
	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) {
			if err := os.WriteFile(args[i+1], []byte("binary"), 0o755); err != nil {
				return execx.Result{}, err
			}
		}
	}
	return execx.Result{}, nil
}

func testPlan() *Plan {
	return &Plan{
		Name:    "demo",
		Main:    "cmd/demo",
		Targets: []Target{{OS: "linux", Arch: "amd64"}, {OS: "windows", Arch: "amd64"}},
		Ldflags: []string{"-X main.version={{version}}"},
	}
}

func TestBuildCreatesArchivesAndChecksums(t *testing.T) {
	runner := &fakeEnvRunner{}
	outDir := filepath.Join(t.TempDir(), "assets")
	files, err := testPlan().Build(context.Background(), runner, t.TempDir(), "v1.2.3", outDir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %v", files)
	}
	if filepath.Base(files[2]) != "checksums.txt" {
		t.Fatalf("checksums must be uploaded last: %v", files)
	}
	if filepath.Base(files[0]) != "demo_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("unexpected archive name: %s", files[0])
	}
	if filepath.Base(files[1]) != "demo_1.2.3_windows_amd64.zip" {
		t.Fatalf("unexpected archive name: %s", files[1])
	}

	entries := tarEntries(t, files[0])
	if len(entries) != 1 || entries[0] != "demo" {
		t.Fatalf("unexpected tar entries: %v", entries)
	}
	zipEntries := zipEntries(t, files[1])
	if len(zipEntries) != 1 || zipEntries[0] != "demo.exe" {
		t.Fatalf("unexpected zip entries: %v", zipEntries)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("expected two builds, got %d", len(runner.calls))
	}
	first := runner.calls[0]
	if first.Dir == "" {
		t.Fatal("build must run in the repository root")
	}
	if !containsArg(first.Args, "-trimpath") || !containsArg(first.Args, "-buildvcs=false") {
		t.Fatalf("missing build flags: %v", first.Args)
	}
	if !containsEnv(first.Env, "GOOS=linux") || !containsEnv(first.Env, "CGO_ENABLED=0") {
		t.Fatalf("unexpected env: %v", first.Env)
	}
	if !hasLdflags(first.Args, "-X main.version=1.2.3") {
		t.Fatalf("version was not injected: %v", first.Args)
	}
	if !containsArg(first.Args, "cmd/demo") && !containsArg(first.Args, "./cmd/demo") {
		t.Fatalf("main package missing from build args: %v", first.Args)
	}

	sum, err := shaFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), sum+"  demo_1.2.3_linux_amd64.tar.gz") {
		t.Fatalf("checksums.txt is missing the archive hash:\n%s", data)
	}
}

func TestBuildFailureIsWrapped(t *testing.T) {
	runner := &fakeEnvRunner{err: errors.New("boom")}
	_, err := testPlan().Build(context.Background(), runner, t.TempDir(), "1.2.3", filepath.Join(t.TempDir(), "assets"))
	if err == nil || !strings.Contains(err.Error(), "build linux/amd64") {
		t.Fatalf("expected a target-aware error, got %v", err)
	}
}

type plainRunner struct{}

func (plainRunner) Run(context.Context, string, string, ...string) (execx.Result, error) {
	return execx.Result{}, nil
}

func TestBuildRequiresEnvRunner(t *testing.T) {
	_, err := testPlan().Build(context.Background(), plainRunner{}, t.TempDir(), "1.2.3", filepath.Join(t.TempDir(), "assets"))
	if err == nil {
		t.Fatal("expected an error when the runner cannot set env")
	}
}

func tarEntries(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if header.Mode != 0o755 {
			t.Fatalf("mode = %o", header.Mode)
		}
	}
	return names
}

func zipEntries(t *testing.T, path string) []string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}

func shaFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func hasLdflags(args []string, want string) bool {
	for i, arg := range args {
		if arg == "-ldflags" && i+1 < len(args) && args[i+1] == want {
			return true
		}
	}
	return false
}
