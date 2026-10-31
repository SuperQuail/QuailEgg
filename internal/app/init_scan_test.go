package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func readInitConfig(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".quailegg.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}

func TestInitDefaultsToAllScannedPackages(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
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
	for _, want := range []string{`[packages."npm@root-pkg"]`, `name = "root-pkg"`, `[packages."npm@web-pkg"]`, `version_scope = "shared"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("config is missing %s:\n%s", want, text)
		}
	}
}

func TestInitRegistersOnlySelectedPackages(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"package.json":     `{"name":"root-pkg","version":"1.0.0"}`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{"npm:web"},
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	text := readInitConfig(t, dir)
	if !strings.Contains(text, `[packages."npm@web-pkg"]`) {
		t.Fatalf("selected package missing:\n%s", text)
	}
	if strings.Contains(text, "root-pkg") {
		t.Fatalf("unselected package was registered:\n%s", text)
	}
}

func TestInitRejectsUnknownPackage(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"package.json": `{"name":"root-pkg","version":"1.0.0"}`,
	})
	_, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{"npm:missing"},
		QeggVersion:   "latest",
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v, want an unknown package error", err)
	}
}

func TestInitRejectsUnselectablePackage(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"package.json": `{"name":"scripts-only"}`,
	})
	_, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{"npm:."},
		QeggVersion:   "latest",
	})
	if err == nil || !strings.Contains(err.Error(), "register") {
		t.Fatalf("err = %v, want an unselectable package error", err)
	}
}

func TestInitEmptySelectionNeedsExistingConfig(t *testing.T) {
	fresh := newInitRepo(t)
	writeInitFiles(t, fresh, map[string]string{
		"package.json": `{"name":"root-pkg","version":"1.0.0"}`,
	})
	if _, err := Init(context.Background(), fresh, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{},
		QeggVersion:   "latest",
	}); err == nil {
		t.Fatal("expected an error when a fresh repository registers nothing")
	}

	existing := newInitRepo(t)
	writeInitFiles(t, existing, map[string]string{
		".quailegg.toml": `schema_version = 1

[project]
default_branch = "master"

[packages."root-pkg"]
path = "."
resolver = "npm"
`,
		"package.json": `{"name":"root-pkg","version":"1.0.0"}`,
	})
	plan, err := Init(context.Background(), existing, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{},
		QeggVersion:   "latest",
	})
	if err != nil {
		t.Fatalf("Init with existing config: %v", err)
	}
	if len(plan.Added) != 0 {
		t.Fatalf("added packages = %v, want none", plan.Added)
	}
}

func TestInitSelectionDrivesDerivations(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml":       "[package]\nname = \"solo\"\nversion = \"0.1.0\"\n",
		"web/package.json": `{"name":"web-pkg","version":"2.0.0"}`,
	})
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{"npm:web"},
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	text := readInitConfig(t, dir)
	for _, want := range []string{`registries = ["npm"]`, `scope = ["web"]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("config is missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "cargo test") || strings.Contains(text, `[packages."rust@solo"]`) {
		t.Fatalf("unselected cargo package leaked into the config:\n%s", text)
	}
}

func TestInitWarnsWhenVersionFileIsSkipped(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml":                "[package]\nname = \"sc2\"\nversion = \"0.1.0\"\n",
		"src-tauri/tauri.conf.json": "{\n  \"version\": \"0.1.0\"\n}\n",
		"web/package.json":          `{"name":"web-pkg","version":"2.0.0"}`,
	})
	plan, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		QeggVersion:   "latest",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "src-tauri/tauri.conf.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want a skipped version file warning", plan.Warnings)
	}
	if strings.Contains(readInitConfig(t, dir), "[[version.file]]") {
		t.Fatal("package scope with several packages must not register a version file")
	}
}

func TestInitDepthZeroKeepsWorkspaceDetection(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"package.json":     `{"name":"root-pkg","version":"1.0.0"}`,
		"web/package.json": `{"name":"web-pkg","version":"1.0.0"}`,
	})
	if _, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     0,
		QeggVersion:   "latest",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	text := readInitConfig(t, dir)
	if strings.Contains(text, "web-pkg") {
		t.Fatalf("depth 0 must not scan nested manifests:\n%s", text)
	}
}

func TestInitWarnsAboutUnregisteredWorkspaceMembers(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"Cargo.toml": `[workspace]
members = ["crates/selected-member", "crates/unused-member"]

[workspace.package]
version = "0.1.0"
`,
		"crates/selected-member/Cargo.toml": "[package]\nname = \"selected-member\"\nversion.workspace = true\n",
		"crates/unused-member/Cargo.toml":   "[package]\nname = \"unused-member\"\nversion.workspace = true\n",
	})
	plan, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		Packages:      []string{"rust:crates/selected-member"},
		QeggVersion:   "latest",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "unused-member") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want a workspace version warning", plan.Warnings)
	}
}

func TestInitWarnsAboutSkippedCandidates(t *testing.T) {
	dir := newInitRepo(t)
	writeInitFiles(t, dir, map[string]string{
		"package.json":       `{"name":"root-pkg","version":"1.0.0"}`,
		"tools/package.json": `{"name":"tools-pkg"}`,
	})
	plan, err := Init(context.Background(), dir, ui.Discard(), InitOptions{
		WithWorkflows: false,
		WithHook:      false,
		AddPackages:   true,
		ScanDepth:     3,
		QeggVersion:   "latest",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "qegg scan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want a skipped candidate warning", plan.Warnings)
	}
	text := readInitConfig(t, dir)
	if strings.Contains(text, "tools-pkg") {
		t.Fatalf("versionless package was registered:\n%s", text)
	}
}
