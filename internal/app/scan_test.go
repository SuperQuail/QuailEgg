package app

import (
	"context"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func newScanRepo(t *testing.T) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "init@example.com")
	runGit(t, dir, "config", "user.name", "init")
	return dir
}

func scanFind(result *ScanResult, id string) (ScanPackage, bool) {
	for _, pkg := range result.Packages {
		if pkg.ID == id {
			return pkg, true
		}
	}
	return ScanPackage{}, false
}

func TestScanMarksRegisteredPackages(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		".quailegg.toml": `schema_version = 1

[project]
default_branch = "master"

[packages."root-pkg"]
path = "."
resolver = "npm"
`,
		"package.json":     `{"name":"root-pkg","version":"1.0.0"}`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})

	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	rootPkg, ok := scanFind(result, "npm:.")
	if !ok || !rootPkg.Registered || !rootPkg.Selectable {
		t.Fatalf("registered package = %+v", rootPkg)
	}
	web, ok := scanFind(result, "npm:web")
	if !ok || web.Registered || !web.Selectable {
		t.Fatalf("new package = %+v", web)
	}
	if len(result.DefaultPackages) != 1 || result.DefaultPackages[0] != "npm:web" {
		t.Fatalf("default packages = %v, want [npm:web]", result.DefaultPackages)
	}
	if !result.Project.RemoteKnown || result.Project.Repo != "github.com/acme/demo" {
		t.Fatalf("project = %+v", result.Project)
	}
}

func TestScanMarksVersionlessPackages(t *testing.T) {
	dir := newScanRepo(t)
	write := func(rel, body string) {
		writeInitFiles(t, dir, map[string]string{rel: body})
	}
	write("package.json", `{"name":"scripts-only"}`)
	write("web/package.json", `{"name":"web-pkg","version":"1.0.0"}`)

	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	rootPkg, ok := scanFind(result, "npm:.")
	if !ok || rootPkg.Selectable || rootPkg.Reason != "no_version" {
		t.Fatalf("versionless package = %+v", rootPkg)
	}
	if _, ok := scanFind(result, "npm:web"); !ok {
		t.Fatalf("web package missing: %+v", result.Packages)
	}
	if len(result.DefaultPackages) != 1 || result.DefaultPackages[0] != "npm:web" {
		t.Fatalf("default packages = %v", result.DefaultPackages)
	}
}

func TestScanMarksNameConflicts(t *testing.T) {
	dir := newScanRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"a/package.json": `{"name":"dup","version":"1.0.0"}`,
		"b/package.json": `{"name":"dup","version":"1.0.0"}`,
	})

	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, id := range []string{"npm:a", "npm:b"} {
		pkg, ok := scanFind(result, id)
		if !ok || pkg.Selectable || pkg.Reason != "duplicate_name" {
			t.Fatalf("%s = %+v, want a duplicate name", id, pkg)
		}
	}
	if len(result.DefaultPackages) != 0 {
		t.Fatalf("default packages = %v, want none", result.DefaultPackages)
	}
}

func TestScanRegistersConfiguredElsewhereAsConflict(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		".quailegg.toml": `schema_version = 1

[project]
default_branch = "master"

[packages."web-pkg"]
path = "old"
resolver = "npm"
`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})

	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	web, ok := scanFind(result, "npm:web")
	if !ok || web.Selectable || web.Reason != "duplicate_name" {
		t.Fatalf("web = %+v, want a duplicate name", web)
	}
}

func TestScanPredictsVersionScope(t *testing.T) {
	root := newScanRepo(t)
	writeInitFiles(t, root, map[string]string{
		"a/package.json": `{"name":"a","version":"2.0.0"}`,
		"b/package.json": `{"name":"b","version":"2.0.0"}`,
	})
	result, err := Scan(context.Background(), root, ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.VersionScope != "shared" {
		t.Fatalf("version scope = %q, want shared", result.VersionScope)
	}

	mixed := newScanRepo(t)
	writeInitFiles(t, mixed, map[string]string{
		"a/package.json": `{"name":"a","version":"2.0.0"}`,
		"b/package.json": `{"name":"b","version":"2.1.0"}`,
	})
	result, err = Scan(context.Background(), mixed, ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.VersionScope != "package" {
		t.Fatalf("version scope = %q, want package", result.VersionScope)
	}
}

func TestScanListsVersionFiles(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml":                "[package]\nname = \"sc2\"\nversion = \"0.1.0\"\n",
		"src-tauri/tauri.conf.json": "{\n  \"version\": \"0.1.0\"\n}\n",
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.VersionFiles) != 1 || !result.VersionFiles[0].Selectable {
		t.Fatalf("version files = %+v", result.VersionFiles)
	}
	if result.VersionFiles[0].Path != "src-tauri/tauri.conf.json" {
		t.Fatalf("version file path = %q", result.VersionFiles[0].Path)
	}
}

func TestScanVersionFileNeedsSharedScope(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml":                "[package]\nname = \"sc2\"\nversion = \"0.1.0\"\n",
		"src-tauri/tauri.conf.json": "{\n  \"version\": \"0.1.0\"\n}\n",
		"web/package.json":          `{"name":"web-pkg","version":"2.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.VersionFiles) != 1 {
		t.Fatalf("version files = %+v", result.VersionFiles)
	}
	entry := result.VersionFiles[0]
	if entry.Selectable || entry.Reason != "needs_shared_scope" {
		t.Fatalf("version file = %+v, want needs_shared_scope", entry)
	}
}

func TestScanWarnsWithoutRemote(t *testing.T) {
	dir := newScanRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"package.json": `{"name":"solo","version":"1.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.Project.RemoteKnown {
		t.Fatalf("project = %+v, want an unknown remote", result.Project)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for the missing remote and config")
	}
}

func TestSelectPackages(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		".quailegg.toml": `schema_version = 1

[project]
default_branch = "master"

[packages."root-pkg"]
path = "."
resolver = "npm"
`,
		"package.json":     `{"name":"root-pkg","version":"1.0.0"}`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if ids, err := SelectPackages(result, nil); err != nil || ids != nil {
		t.Fatalf("nil selectors = %v, %v", ids, err)
	}
	ids, err := SelectPackages(result, []string{"", "npm:web", "web-pkg", "npm:web"})
	if err != nil {
		t.Fatalf("SelectPackages: %v", err)
	}
	if len(ids) != 1 || ids[0] != "npm:web" {
		t.Fatalf("ids = %v, want [npm:web]", ids)
	}
	registered, err := SelectPackages(result, []string{"root-pkg"})
	if err != nil || len(registered) != 1 || registered[0] != "npm:." {
		t.Fatalf("registered selector = %v, %v", registered, err)
	}
	if _, err := SelectPackages(result, []string{"npm:missing"}); err == nil {
		t.Fatal("expected an unknown package error")
	}
}

func TestSelectPackagesRejectsAmbiguityAndUnselectable(t *testing.T) {
	dir := newScanRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"a/package.json": `{"name":"dup","version":"1.0.0"}`,
		"b/package.json": `{"name":"dup","version":"1.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := SelectPackages(result, []string{"dup"}); err == nil || !strings.Contains(err.Error(), "dup") {
		t.Fatalf("ambiguous selector err = %v", err)
	}

	versionless := newScanRepo(t)
	writeInitFiles(t, versionless, map[string]string{
		"package.json": `{"name":"scripts-only"}`,
	})
	result, err = Scan(context.Background(), versionless, ScanOptions{Depth: 1})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := SelectPackages(result, []string{"npm:."}); err == nil {
		t.Fatal("expected an unselectable package error")
	}
}

func TestScanUsesDefaultRemoteFromConfig(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		".quailegg.toml": `schema_version = 1

[project]
default_branch = "master"

[git]
remote = "origin"

[packages."root-pkg"]
path = "."
resolver = "npm"
`,
		"package.json": `{"name":"root-pkg","version":"1.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.Project.Remote != "origin" || !result.Project.RemoteKnown {
		t.Fatalf("project = %+v", result.Project)
	}
}
