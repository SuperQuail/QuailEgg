package golang_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/golang"
)

func TestSpec(t *testing.T) {
	spec := golang.New().Spec()
	if spec.ID != "go" || spec.Language != "Go" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if len(spec.Manifests) != 1 || spec.Manifests[0] != "go.mod" {
		t.Fatalf("unexpected manifests: %v", spec.Manifests)
	}
	if len(spec.PublishTargets) != 1 || spec.PublishTargets[0] != "go" || spec.TagPrefix != "v" {
		t.Fatalf("unexpected capabilities: %+v", spec)
	}
	if spec.WorkflowSetup != "" || spec.WorkflowEnv != "" {
		t.Fatalf("go must not add workflow fragments: %+v", spec)
	}
	if golang.New().Publisher().Name() != "go" {
		t.Fatal("publisher name mismatch")
	}
}

func TestDetectModule(t *testing.T) {
	root := ecosystemtest.Fixture(t, "go-basic")
	pkgs, err := golang.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected one module, got %+v", pkgs)
	}
	pkg := pkgs[0]
	if pkg.Name != "example.com/demo" || pkg.Path != "." || pkg.Resolver != "go" {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if pkg.Manifest != "go.mod" || !pkg.Publish {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if pkg.Version != "" {
		t.Fatalf("go versions come from tags, want empty manifest version, got %q", pkg.Version)
	}
}

func TestDetectWithoutGoMod(t *testing.T) {
	pkgs, err := golang.New().Detect(t.TempDir())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected no packages, got %+v", pkgs)
	}
}

func TestDetectMalformedModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("go 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := golang.New().Detect(root)
	if err == nil || !strings.Contains(err.Error(), "module directive") {
		t.Fatalf("expected a missing module error, got %v", err)
	}
}

func TestSetVersionIsNoop(t *testing.T) {
	root := ecosystemtest.Fixture(t, "go-basic")
	adapter := golang.New()
	pkgs, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetVersion(root, pkgs[0], "1.4.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("go.mod must not change:\n%s", after)
	}
}
