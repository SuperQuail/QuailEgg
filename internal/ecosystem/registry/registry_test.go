package registry_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
)

func TestSpecsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range registry.Specs() {
		if spec.ID == "" || spec.Language == "" {
			t.Errorf("incomplete spec: %+v", spec)
		}
		if seen[spec.ID] {
			t.Errorf("duplicate spec id %q", spec.ID)
		}
		seen[spec.ID] = true
		if len(spec.Manifests) == 0 {
			t.Errorf("spec %q has no manifest", spec.ID)
		}
		if len(spec.Tools) == 0 {
			t.Errorf("spec %q has no tools", spec.ID)
		}
	}
}

func TestEverySpecHasAnEcosystem(t *testing.T) {
	for _, spec := range registry.Specs() {
		eco, ok := registry.ByID(spec.ID)
		if !ok {
			t.Errorf("spec %q has no ecosystem", spec.ID)
			continue
		}
		if eco.Publisher() == nil {
			t.Errorf("ecosystem %q has no publisher", spec.ID)
		}
	}
}

func TestManifestListText(t *testing.T) {
	text := registry.ManifestListText()
	for _, want := range []string{"package.json", "pyproject.toml", "Cargo.toml", "go.mod"} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest list %q is missing %q", text, want)
		}
	}
}

func TestVersionSources(t *testing.T) {
	if !registry.IsTagVersioned("go") {
		t.Error("go must derive its version from tags")
	}
	if registry.IsTagVersioned("npm") {
		t.Error("npm keeps its version in the manifest")
	}
	pkg := ecosystem.Package{Name: "demo", Resolver: "npm", Version: "1.2.3"}
	version, err := registry.CurrentVersion(context.Background(), nil, t.TempDir(), pkg, "v")
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("version = %q, want the manifest version", version)
	}
	if err := registry.CheckVersion(pkg, "2.0.0"); err != nil {
		t.Fatalf("manifest ecosystems must not constrain majors: %v", err)
	}
}

func TestDetectAll(t *testing.T) {
	root := ecosystemtest.Fixture(t, "npm-workspace")
	pkgs, err := registry.DetectAll(root)
	if err != nil {
		t.Fatalf("DetectAll: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %+v", pkgs)
	}
	if _, ok := registry.ByID("unknown"); ok {
		t.Fatal("unknown resolver must not resolve")
	}
}
