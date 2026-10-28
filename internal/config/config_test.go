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

func TestUnknownField(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[project]\ndefault_branch = \"main\"\nunknown_key = true\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for an unknown key")
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

func TestGoResolverAndRegistry(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[packages.main]\npath = \".\"\nresolver = \"go\"\n\n[publish]\nregistries = [\"go\"]\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if pkg, ok := cfg.Packages["main"]; !ok || pkg.Resolver != ResolverGo {
		t.Fatalf("unexpected packages: %+v", cfg.Packages)
	}
	if len(cfg.Publish.Registries) != 1 || cfg.Publish.Registries[0] != ResolverGo {
		t.Fatalf("unexpected registries: %v", cfg.Publish.Registries)
	}
}

func TestLegacyLanguageKeyAccepted(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\nlanguage = \"en\"\n")
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("legacy language key must keep loading: %v", err)
	}
}
