package config

import "testing"

func TestManifestNameAndPackageKeys(t *testing.T) {
	body := `schema_version = 1

[project]
default_branch = "main"

[packages."npm@app"]
name = "app"
path = "."
resolver = "npm"

[packages."rust@app"]
name = "app"
path = "src-tauri"
resolver = "rust"

[packages."legacy"]
path = "legacy"
resolver = "npm"
`
	cfg, err := LoadFile(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := cfg.ManifestName("npm@app"); got != "app" {
		t.Fatalf("ManifestName(npm@app) = %q", got)
	}
	if got := cfg.ManifestName("legacy"); got != "legacy" {
		t.Fatalf("ManifestName(legacy) = %q", got)
	}
	if key, ok := cfg.PackageKeyFor("npm@app"); !ok || key != "npm@app" {
		t.Fatalf("PackageKeyFor(npm@app) = %q, %v", key, ok)
	}
	if key, ok := cfg.PackageKeyFor("legacy"); !ok || key != "legacy" {
		t.Fatalf("PackageKeyFor(legacy) = %q, %v", key, ok)
	}
	if _, ok := cfg.PackageKeyFor("app"); ok {
		t.Fatal("an ambiguous manifest name must not resolve to a key")
	}
	if _, ok := cfg.PackageKeyFor("missing"); ok {
		t.Fatal("an unknown package must not resolve")
	}
}
