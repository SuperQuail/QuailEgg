package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
)

func TestInspectCarriesIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"app\",\"version\":\"1.0.0\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := registry.Inspect(root, ecosystem.Package{Name: "app", Identity: "npm@app", Path: ".", Resolver: "npm"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if pkg.Name != "app" {
		t.Fatalf("name = %q", pkg.Name)
	}
	if pkg.IdentityName() != "npm@app" {
		t.Fatalf("identity = %q", pkg.IdentityName())
	}
}

func TestDefaultPublishKeepsTauriCratesOutOfCratesIO(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src-tauri")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"app\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tauri.conf.json"), []byte("{\"version\":\"0.1.0\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := ecosystem.Package{Name: "app", Path: "src-tauri", Resolver: "rust", Publish: true}
	if registry.DefaultPublish(root, pkg) {
		t.Fatal("a Tauri application crate must not publish by default")
	}
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "Cargo.toml"), []byte("[package]\nname = \"lib\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := ecosystem.Package{Name: "lib", Path: "lib", Resolver: "rust", Publish: true}
	if !registry.DefaultPublish(root, plain) {
		t.Fatal("a regular crate keeps its manifest publish decision")
	}
}
