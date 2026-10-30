package rust_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/rust"
)

func TestDetectWorkspaceInheritance(t *testing.T) {
	root := ecosystemtest.Fixture(t, "cargo-workspace")
	pkgs, err := rust.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("packages = %+v", pkgs)
	}
	byName := map[string]ecosystem.Package{}
	for _, pkg := range pkgs {
		byName[pkg.Name] = pkg
	}
	for _, name := range []string{"cw-core", "cw-cli", "cw-app"} {
		pkg, ok := byName[name]
		if !ok {
			t.Fatalf("package %s was not detected", name)
		}
		if pkg.Version != "0.4.0" {
			t.Fatalf("%s version = %q, want the workspace version 0.4.0", name, pkg.Version)
		}
		if !pkg.Inherited {
			t.Fatalf("%s must be marked as inherited", name)
		}
	}
	if byName["cw-app"].Publish {
		t.Fatal("publish = false must not be publishable")
	}
	if !byName["cw-cli"].Publish || !byName["cw-core"].Publish {
		t.Fatal("packages without publish = false stay publishable")
	}
	if !slices.Contains(byName["cw-cli"].Deps, "cw-core") {
		t.Fatalf("cw-cli deps = %v, want cw-core", byName["cw-cli"].Deps)
	}
	if got := byName["cw-cli"].Manifest; got != "crates/cli/Cargo.toml" {
		t.Fatalf("manifest = %q", got)
	}
}

func TestDetectMissingWorkspaceVersion(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "Cargo.toml", "[workspace]\nmembers = [\"crates/*\"]\n")
	writeManifest(t, root, "crates/core/Cargo.toml", "[package]\nname = \"orphan\"\nversion.workspace = true\n")
	_, err := rust.New().Detect(root)
	if err == nil || !strings.Contains(err.Error(), "workspace.package") {
		t.Fatalf("err = %v, want a workspace.package error", err)
	}
}

func TestDetectPublishValues(t *testing.T) {
	cases := map[string]struct {
		publish string
		want    bool
	}{
		"omitted":     {publish: "", want: true},
		"false":       {publish: "publish = false\n", want: false},
		"crates-io":   {publish: "publish = [\"crates-io\"]\n", want: true},
		"other-index": {publish: "publish = [\"private\"]\n", want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, "Cargo.toml", "[package]\nname = \"solo\"\nversion = \"1.0.0\"\n"+tc.publish)
			pkgs, err := rust.New().Detect(root)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if len(pkgs) != 1 || pkgs[0].Publish != tc.want {
				t.Fatalf("packages = %+v, want publish %v", pkgs, tc.want)
			}
		})
	}
}

func TestDetectResolvesRenamedDependencies(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "Cargo.toml", `[workspace]
members = ["crates/*"]

[workspace.package]
version = "0.2.0"
`)
	writeManifest(t, root, "crates/core/Cargo.toml", "[package]\nname = \"renamed-core\"\nversion.workspace = true\n")
	writeManifest(t, root, "crates/app/Cargo.toml", `[package]
name = "renamed-app"
version.workspace = true

[dependencies]
alias = { package = "renamed-core", path = "../core" }
`)
	pkgs, err := rust.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	found := false
	for _, pkg := range pkgs {
		if pkg.Name == "renamed-app" {
			found = true
			if !slices.Contains(pkg.Deps, "renamed-core") {
				t.Fatalf("deps = %v, want the real package name", pkg.Deps)
			}
		}
	}
	if !found {
		t.Fatalf("renamed-app was not detected: %+v", pkgs)
	}
}

func TestSetVersionSkipsInheritedMembers(t *testing.T) {
	root := ecosystemtest.Fixture(t, "cargo-workspace")
	adapter := rust.New()
	pkgs, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "crates", "core", "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		if err := adapter.SetVersion(root, pkg, "0.9.0"); err != nil {
			t.Fatalf("SetVersion(%s): %v", pkg.Name, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, "crates", "core", "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("inherited manifest changed:\n%s", after)
	}
	updated, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range updated {
		if pkg.Version != "0.4.0" {
			t.Fatalf("%s version changed to %q", pkg.Name, pkg.Version)
		}
	}
}

func writeManifest(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
