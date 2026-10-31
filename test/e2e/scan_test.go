package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeE2EFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type scanReport struct {
	SchemaVersion int `json:"schema_version"`
	Project       struct {
		RemoteKnown bool `json:"remote_known"`
	} `json:"project"`
	Packages []struct {
		ID         string `json:"id"`
		Registered bool   `json:"registered"`
		Selectable bool   `json:"selectable"`
		Source     string `json:"source"`
	} `json:"packages"`
	DefaultPackages []string `json:"default_packages"`
	Warnings        []string `json:"warnings"`
}

func TestScanReportsNestedPackagesWithoutWriting(t *testing.T) {
	bin := buildBinary(t)
	dir := newRepo(t)
	writeE2EFile(t, dir, ".gitignore", "node_modules/\n")
	writeE2EFile(t, dir, "web/package.json", "{\"name\":\"e2e-web\",\"version\":\"1.0.0\"}\n")
	writeE2EFile(t, dir, "node_modules/noise/package.json", "{\"name\":\"noise\",\"version\":\"1.0.0\"}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "add nested package")

	out := runStrict(t, bin, dir, "scan", "--json")
	var report scanReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("scan --json: %v\n%s", err, out)
	}
	if report.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", report.SchemaVersion)
	}
	foundWeb := false
	for _, pkg := range report.Packages {
		if pkg.ID != "npm:web" {
			continue
		}
		foundWeb = true
		if pkg.Source != "scan" || !pkg.Selectable || pkg.Registered {
			t.Fatalf("nested package = %+v, want a selectable scanned candidate", pkg)
		}
	}
	if !foundWeb {
		t.Fatalf("nested package missing from %+v", report.Packages)
	}
	if len(report.DefaultPackages) != 2 {
		t.Fatalf("default packages = %v, want both candidates", report.DefaultPackages)
	}
	if report.Project.RemoteKnown {
		t.Fatal("scan must not require a remote")
	}
	if _, err := os.Stat(filepath.Join(dir, ".quailegg.toml")); err == nil {
		t.Fatal("scan must not write a configuration")
	}
	if _, err := os.Stat(filepath.Join(dir, ".changes")); err == nil {
		t.Fatal("scan must not create the change record directory")
	}
}

func TestInitRegistersSelectedScannedPackage(t *testing.T) {
	bin := buildBinary(t)
	dir := newRepo(t)
	writeE2EFile(t, dir, "web/package.json", "{\"name\":\"e2e-web\",\"version\":\"1.0.0\"}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "add web package")
	addOrigin(t, dir)

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "npm",
		"--packages", "npm:web", "--no-workflows", "--no-hook")
	data, err := os.ReadFile(filepath.Join(dir, ".quailegg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `[packages."npm@e2e-web"]`) {
		t.Fatalf("selected package missing:\n%s", text)
	}
	if strings.Contains(text, `[packages."npm@e2e-pkg"]`) {
		t.Fatalf("unselected package was registered:\n%s", text)
	}
}

func TestInitRegistersAllScannedPackagesByDefault(t *testing.T) {
	bin := buildBinary(t)
	dir := newRepo(t)
	writeE2EFile(t, dir, "web/package.json", "{\"name\":\"e2e-web\",\"version\":\"1.0.0\"}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "add web package")
	addOrigin(t, dir)

	runStrict(t, bin, dir, "init", "--no-input", "--mode", "commit", "--registries", "npm",
		"--no-workflows", "--no-hook")
	data, err := os.ReadFile(filepath.Join(dir, ".quailegg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`[packages."npm@e2e-pkg"]`, `[packages."npm@e2e-web"]`, `version_scope = "shared"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("config is missing %s:\n%s", want, text)
		}
	}
}
