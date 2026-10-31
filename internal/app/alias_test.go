package app

import (
	"context"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func writeAliasFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	writeInitFiles(t, dir, files)
}

func TestScanSuggestsAliasedKeys(t *testing.T) {
	dir := newInitRepo(t)
	writeAliasFiles(t, dir, map[string]string{
		"package.json":              `{"name":"app","version":"0.1.0","private":true}`,
		"src-tauri/Cargo.toml":      "[package]\nname = \"app\"\nversion = \"0.1.0\"\n",
		"src-tauri/tauri.conf.json": `{"version":"0.1.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	npmPkg, ok := scanFind(result, "npm:.")
	if !ok || npmPkg.Key != "npm@app" || !npmPkg.Selectable || npmPkg.Publish {
		t.Fatalf("npm candidate = %+v", npmPkg)
	}
	rustPkg, ok := scanFind(result, "rust:src-tauri")
	if !ok || rustPkg.Key != "rust@app" || !rustPkg.Selectable || rustPkg.Publish {
		t.Fatalf("rust candidate = %+v", rustPkg)
	}
	if len(result.DefaultPackages) != 2 {
		t.Fatalf("default packages = %v, want both", result.DefaultPackages)
	}
	if result.VersionScope != "shared" {
		t.Fatalf("version scope = %q, want shared", result.VersionScope)
	}
	if len(result.VersionFiles) != 1 || result.VersionFiles[0].Path != "src-tauri/tauri.conf.json" {
		t.Fatalf("version files = %+v", result.VersionFiles)
	}
}

func TestScanStripsOneLeadingAtFromScopedNames(t *testing.T) {
	dir := newInitRepo(t)
	writeAliasFiles(t, dir, map[string]string{
		"package.json": `{"name":"@acme/core","version":"1.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 1})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	pkg, ok := scanFind(result, "npm:.")
	if !ok || pkg.Name != "@acme/core" || pkg.Key != "npm@acme/core" {
		t.Fatalf("candidate = %+v", pkg)
	}
}

func TestScanRejectsUsedKeyForAnotherPackage(t *testing.T) {
	dir := newInitRepo(t)
	writeAliasFiles(t, dir, map[string]string{
		".quailegg.toml": `schema_version = 1

[project]
default_branch = "master"

[packages."npm@web-pkg"]
name = "other-pkg"
path = "old"
resolver = "npm"
`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})
	result, err := Scan(context.Background(), dir, ScanOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	pkg, ok := scanFind(result, "npm:web")
	if !ok || pkg.Selectable || pkg.Reason != "key_conflict" {
		t.Fatalf("candidate = %+v, want key_conflict", pkg)
	}
}

func TestInitRegistersAliasedTauriPackages(t *testing.T) {
	dir := newInitRepo(t)
	writeAliasFiles(t, dir, map[string]string{
		"package.json":              `{"name":"app","version":"0.1.0","private":true}`,
		"src-tauri/Cargo.toml":      "[package]\nname = \"app\"\nversion = \"0.1.0\"\n",
		"src-tauri/tauri.conf.json": `{"version":"0.1.0"}`,
	})
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	text := readInitConfig(t, dir)
	for _, want := range []string{
		`[packages."npm@app"]`,
		`[packages."rust@app"]`,
		`name = "app"`,
		`version_scope = "shared"`,
		`path = "src-tauri/tauri.conf.json"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config is missing %s:\n%s", want, text)
		}
	}
	if strings.Count(text, "publish = false") != 2 {
		t.Fatalf("both app packages must be private:\n%s", text)
	}
	if strings.Count(text, "[[version.file]]") != 1 {
		t.Fatalf("only the Tauri configuration is a version file:\n%s", text)
	}
	for _, key := range []string{"npm@app", "rust@app"} {
		if !strings.Contains(text, `[packages."`+key+`"]`) {
			t.Fatalf("missing package key %s:\n%s", key, text)
		}
	}
}

func TestInitPreservesLegacyKeys(t *testing.T) {
	dir := newInitRepo(t)
	writeAliasFiles(t, dir, map[string]string{
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
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	text := readInitConfig(t, dir)
	if !strings.Contains(text, `[packages."root-pkg"]`) {
		t.Fatalf("legacy key was rewritten:\n%s", text)
	}
	if !strings.Contains(text, `[packages."npm@web-pkg"]`) || !strings.Contains(text, `name = "web-pkg"`) {
		t.Fatalf("new package was not keyed with its resolver:\n%s", text)
	}
}

func TestInitAcceptsAliasedSelector(t *testing.T) {
	dir := newInitRepo(t)
	writeAliasFiles(t, dir, map[string]string{
		"package.json":     `{"name":"root-pkg","version":"1.0.0"}`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{"npm@web-pkg"},
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	text := readInitConfig(t, dir)
	if !strings.Contains(text, `[packages."npm@web-pkg"]`) {
		t.Fatalf("selected alias missing:\n%s", text)
	}
	if strings.Contains(text, "root-pkg") {
		t.Fatalf("unselected package was registered:\n%s", text)
	}
}
