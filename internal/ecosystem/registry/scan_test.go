package registry_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findScanned(found []registry.Scanned, resolver, path string) (registry.Scanned, bool) {
	for _, entry := range found {
		if entry.Package.Resolver == resolver && entry.Package.Path == path {
			return entry, true
		}
	}
	return registry.Scanned{}, false
}

func TestScanFindsNestedManifest(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "web/package.json", `{"name":"web-pkg","version":"1.0.0"}`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 1})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	web, ok := findScanned(found, "npm", "web")
	if !ok {
		t.Fatalf("nested manifest not found: %+v", found)
	}
	if web.Source != registry.ScanSourceFile || web.Reason != "" {
		t.Fatalf("unexpected candidate: %+v", web)
	}

	rootOnly, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 0})
	if err != nil {
		t.Fatalf("Scan depth 0: %v", err)
	}
	if _, ok := findScanned(rootOnly, "npm", "web"); ok {
		t.Fatalf("depth 0 must keep workspace detection only: %+v", rootOnly)
	}
}

func TestScanRespectsIgnores(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, ".gitignore", "ignored/\n")
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "ignored/package.json", `{"name":"ignored-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "node_modules/left-pad/package.json", `{"name":"left-pad","version":"1.0.0"}`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, ok := findScanned(found, "npm", "ignored"); ok {
		t.Fatalf("gitignored package must not be scanned: %+v", found)
	}
	if entry, ok := findScanned(found, "npm", "node_modules/left-pad"); ok && entry.Reason == "" {
		t.Fatalf("node_modules package must not be selectable: %+v", entry)
	}
}

func TestScanDepthLimit(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "a/b/c/package.json", `{"name":"level-three","version":"1.0.0"}`)
	writeRepoFile(t, root, "a/b/c/d/package.json", `{"name":"level-four","version":"1.0.0"}`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, ok := findScanned(found, "npm", "a/b/c"); !ok {
		t.Fatalf("manifest at depth 3 not found: %+v", found)
	}
	if _, ok := findScanned(found, "npm", "a/b/c/d"); ok {
		t.Fatalf("manifest below depth 3 was scanned: %+v", found)
	}
}

func TestScanExcludePattern(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "apps/web/package.json", `{"name":"web-pkg","version":"1.0.0"}`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 3, Exclude: []string{"apps"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	web, ok := findScanned(found, "npm", "apps/web")
	if !ok || web.Reason != registry.ScanReasonExcluded {
		t.Fatalf("excluded package = %+v, want reason %q", web, registry.ScanReasonExcluded)
	}
}

func TestScanRejectsBadExcludePattern(t *testing.T) {
	root := newGitRepo(t)
	if _, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 1, Exclude: []string{"["}}); err == nil {
		t.Fatal("expected an error for an invalid glob")
	}
}

func TestScanNestedGoUnsupported(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "go.mod", "module example.com/root\n\ngo 1.22\n")
	writeRepoFile(t, root, "tools/go.mod", "module example.com/tools\n\ngo 1.22\n")

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	tools, ok := findScanned(found, "go", "tools")
	if !ok || tools.Reason != ecosystem.ScanReasonNestedGo {
		t.Fatalf("nested go module = %+v, want reason %q", tools, ecosystem.ScanReasonNestedGo)
	}
}

func TestScanNestedCargoWorkspaceUnsupported(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "frontend/Cargo.toml", `[workspace]
members = ["crates/core"]

[workspace.package]
version = "0.2.0"
`)
	writeRepoFile(t, root, "frontend/crates/core/Cargo.toml", `[package]
name = "nested-core"
version.workspace = true
`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	core, ok := findScanned(found, "rust", "frontend/crates/core")
	if !ok || core.Reason != ecosystem.ScanReasonNestedWorkspace {
		t.Fatalf("nested workspace member = %+v, want reason %q", core, ecosystem.ScanReasonNestedWorkspace)
	}
}

func TestScanInvalidNestedManifest(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "broken/package.json", "{not json")

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	broken, ok := findScanned(found, "npm", "broken")
	if !ok || broken.Reason != registry.ScanReasonInvalidManifest {
		t.Fatalf("broken manifest = %+v, want reason %q", broken, registry.ScanReasonInvalidManifest)
	}
}

func TestScanWorkspaceCandidateWins(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0","workspaces":["packages/*"]}`)
	writeRepoFile(t, root, "packages/core/package.json", `{"name":"core","version":"1.0.0"}`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	count := 0
	for _, entry := range found {
		if entry.Package.Resolver == "npm" && entry.Package.Path == "packages/core" {
			count++
			if entry.Source != registry.ScanSourceWorkspace {
				t.Fatalf("duplicate scan candidate won: %+v", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("packages/core candidates = %d, want 1: %+v", count, found)
	}
}

func TestScanSortsCandidates(t *testing.T) {
	root := newGitRepo(t)
	writeRepoFile(t, root, "package.json", `{"name":"root-pkg","version":"1.0.0"}`)
	writeRepoFile(t, root, "zeta/package.json", `{"name":"zeta","version":"1.0.0"}`)
	writeRepoFile(t, root, "alpha/package.json", `{"name":"alpha","version":"1.0.0"}`)

	found, err := registry.Scan(context.Background(), root, registry.ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var paths []string
	for _, entry := range found {
		paths = append(paths, entry.Package.Path)
	}
	want := []string{".", "alpha", "zeta"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}
