package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Release.Mode != ModePR {
		t.Errorf("mode = %q, want pr", cfg.Release.Mode)
	}
	if cfg.Git.TagPrefix != "v" || cfg.Changes.Dir != ".changes" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Verify.Timeout != "15m" {
		t.Errorf("verify timeout = %q", cfg.Verify.Timeout)
	}
}

// A newer qegg may write keys this binary does not know yet; the old binary
// has to keep working, so unknown keys are ignored instead of rejected.
func TestUnknownFieldsAreIgnored(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[project]\ndefault_branch = \"main\"\nrepo = \"github.com/a/b\"\nfuture_key = true\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unknown keys must not fail the load: %v", err)
	}
	if cfg.Project.DefaultBranch != "main" {
		t.Fatalf("known keys must still be read: %+v", cfg.Project)
	}
}

func TestInvalidMode(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release]\nmode = \"fast\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for an invalid mode")
	}
}

func TestUnsupportedSchema(t *testing.T) {
	path := writeConfig(t, "schema_version = 2\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for an unsupported schema version")
	}
}

func TestPackagePublishable(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[packages.a]\npath = \".\"\nresolver = \"npm\"\n\n[packages.b]\npath = \"b\"\nresolver = \"npm\"\npublish = false\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.PackagePublishable("a") {
		t.Error("a should be publishable by default")
	}
	if cfg.PackagePublishable("b") {
		t.Error("b should not be publishable")
	}
}

func TestReleaseAssetsConfig(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release.assets]\ntargets = [\"linux/amd64\", \"windows/amd64\"]\nldflags = [\"-X main.version={{version}}\"]\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Release.Assets.Targets) != 2 || cfg.Release.Assets.Targets[1] != "windows/amd64" {
		t.Fatalf("unexpected targets: %v", cfg.Release.Assets.Targets)
	}
	if len(cfg.Release.Assets.Ldflags) != 1 {
		t.Fatalf("unexpected ldflags: %v", cfg.Release.Assets.Ldflags)
	}
}

func TestReleaseAssetsRejectBadTarget(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release.assets]\ntargets = [\"linux\"]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a malformed asset target")
	}
}

func TestReleaseAssetsMainCannotEscape(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release.assets]\nmain = \"../outside\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a main package outside the repository")
	}
}

func TestLegacyLanguageKeyAccepted(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\nlanguage = \"en\"\n")
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("legacy language key must keep loading: %v", err)
	}
}

func TestValidateRejectsReleaseBranchEqualToDefault(t *testing.T) {
	cfg := Default()
	cfg.Release.Branch = cfg.Project.DefaultBranch
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a validation error")
	}
}

func TestProjectRepoValidation(t *testing.T) {
	good := writeConfig(t, "schema_version = 1\n[project]\ndefault_branch = \"main\"\nrepo = \"github.com/SuperQuail/QuailEgg\"\n")
	if _, err := LoadFile(good); err != nil {
		t.Fatalf("a canonical repo identity must load: %v", err)
	}
	bad := writeConfig(t, "schema_version = 1\n[project]\nrepo = \"not-a-repo\"\n")
	if _, err := LoadFile(bad); err == nil {
		t.Fatal("expected an error for a repo without host/owner/name")
	}
}
