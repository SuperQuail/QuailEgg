package python_test

import (
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/python"
)

func TestSpec(t *testing.T) {
	spec := python.New().Spec()
	if spec.ID != "python" || len(spec.Manifests) == 0 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if python.New().Publisher().Name() != "python" {
		t.Fatal("publisher name mismatch")
	}
}

func TestDetectVariants(t *testing.T) {
	cases := []struct {
		fixture string
		name    string
		version string
	}{
		{"python-pep621", "pep-demo", "1.0.0"},
		{"python-poetry", "poetry-demo", "2.1.0"},
		{"python-hatch-dynamic", "hatch-demo", "0.9.0"},
		{"python-setuptools-dynamic", "setuptools-demo", "3.2.1"},
		{"python-prerelease", "prerelease-demo", "2.0.2-a14"},
	}
	for _, tc := range cases {
		root := ecosystemtest.Fixture(t, tc.fixture)
		pkgs, err := python.New().Detect(root)
		if err != nil {
			t.Fatalf("%s: %v", tc.fixture, err)
		}
		if len(pkgs) != 1 || pkgs[0].Name != tc.name || pkgs[0].Version != tc.version {
			t.Fatalf("%s: unexpected packages %+v", tc.fixture, pkgs)
		}
	}
}

func TestDetectUVWorkspace(t *testing.T) {
	root := ecosystemtest.Fixture(t, "python-uv-workspace")
	pkgs, err := python.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	found := map[string]bool{}
	for _, pkg := range pkgs {
		found[pkg.Name] = true
	}
	if !found["uv-root"] || !found["uv-app"] {
		t.Fatalf("unexpected packages: %+v", pkgs)
	}
}

func TestSetVersionStatic(t *testing.T) {
	root := ecosystemtest.Fixture(t, "python-pep621")
	adapter := python.New()
	pkgs, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetVersion(root, pkgs[0], "1.1.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	updated, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].Version != "1.1.0" {
		t.Fatalf("version not updated: %+v", updated[0])
	}
}

func TestSetVersionDynamic(t *testing.T) {
	root := ecosystemtest.Fixture(t, "python-hatch-dynamic")
	adapter := python.New()
	pkgs, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetVersion(root, pkgs[0], "1.0.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	updated, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].Version != "1.0.0" {
		t.Fatalf("version not updated: %+v", updated[0])
	}
}
