package rust_test

import (
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/rust"
)

func TestSpec(t *testing.T) {
	spec := rust.New().Spec()
	if spec.ID != "rust" || len(spec.Manifests) == 0 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if rust.New().Publisher().Name() != "rust" {
		t.Fatal("publisher name mismatch")
	}
}

func TestDetect(t *testing.T) {
	root := ecosystemtest.Fixture(t, "cargo-basic")
	pkgs, err := rust.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "cargo-basic" || pkgs[0].Version != "0.2.0" {
		t.Fatalf("unexpected packages: %+v", pkgs)
	}
}

func TestSetVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "cargo-basic")
	adapter := rust.New()
	pkgs, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetVersion(root, pkgs[0], "0.3.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	updated, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].Version != "0.3.0" {
		t.Fatalf("version not updated: %+v", updated[0])
	}
}

func TestInspectWorkspaceMember(t *testing.T) {
	root := ecosystemtest.Fixture(t, "cargo-workspace")
	inspector, ok := rust.New().(ecosystem.Inspector)
	if !ok {
		t.Fatal("rust adapter does not implement Inspector")
	}
	pkg, err := inspector.Inspect(root, ecosystem.Package{Name: "cw-core", Path: "crates/core", Resolver: "rust"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if pkg.Version != "0.4.0" || !pkg.Inherited || pkg.Manifest != "crates/core/Cargo.toml" {
		t.Fatalf("unexpected package: %+v", pkg)
	}
}
