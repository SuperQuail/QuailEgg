package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func TestCheckReportsUnknownCapabilities(t *testing.T) {
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"bogus-pkg": {Path: ".", Resolver: "bogus"},
	}
	cfg.Publish.Registries = []string{"bogus"}
	a := &App{Root: t.TempDir(), Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	codes := map[string]bool{}
	for _, problem := range report.Problems {
		codes[problem.Code] = true
	}
	if !codes["unknown-resolver"] {
		t.Fatalf("missing unknown-resolver problem: %+v", report.Problems)
	}
	if !codes["unknown-registry"] {
		t.Fatalf("missing unknown-registry problem: %+v", report.Problems)
	}
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

// A version file that lags behind the manifest is a release blocker: the
// published app would advertise a different version than its packages.
func TestCheckReportsVersionFileDrift(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "Cargo.toml", "[package]\nname = \"sc2translater\"\nversion = \"0.1.0\"\n")
	writeRepoFile(t, dir, "src-tauri/tauri.conf.json", "{\n  \"version\": \"0.0.9\"\n}\n")
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{"sc2translater": {Path: ".", Resolver: "rust"}}
	cfg.Version.File = []config.VersionFile{{Path: "src-tauri/tauri.conf.json", Format: "json", Key: "version"}}
	a := &App{Root: dir, Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "version-file-drift") {
		t.Fatalf("missing version-file-drift problem: %+v", report.Problems)
	}
	writeRepoFile(t, dir, "src-tauri/tauri.conf.json", "{\n  \"version\": \"0.1.0\"\n}\n")
	report, err = a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if hasProblem(report, "version-file-drift") {
		t.Fatalf("an aligned version file still reports drift: %+v", report.Problems)
	}
	cfg.Version.File = []config.VersionFile{{Path: "src-tauri/missing.json", Format: "json", Key: "version"}}
	report, err = a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "version-file-invalid") {
		t.Fatalf("missing version-file-invalid problem: %+v", report.Problems)
	}
}

func hasProblem(report *CheckReport, code string) bool {
	for _, problem := range report.Problems {
		if problem.Code == code {
			return true
		}
	}
	return false
}

func TestCheckReportsInspectFailure(t *testing.T) {
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"ui-pkg": {Path: "ui", Resolver: "npm"},
	}
	a := &App{Root: t.TempDir(), Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	codes := map[string]bool{}
	for _, problem := range report.Problems {
		codes[problem.Code] = true
	}
	if !codes["inspect-failed"] {
		t.Fatalf("missing inspect-failed problem: %+v", report.Problems)
	}
}
