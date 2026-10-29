package npm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/npm"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func TestSpec(t *testing.T) {
	spec := npm.New().Spec()
	if spec.ID != "npm" || len(spec.Manifests) == 0 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if npm.New().Publisher().Name() != "npm" {
		t.Fatal("publisher name mismatch")
	}
}

func TestDetectSingle(t *testing.T) {
	root := ecosystemtest.Fixture(t, "npm-basic")
	pkgs, err := npm.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "npm-basic" || pkgs[0].Version != "1.2.3" {
		t.Fatalf("unexpected packages: %+v", pkgs)
	}
}

func TestDetectWorkspace(t *testing.T) {
	root := ecosystemtest.Fixture(t, "npm-workspace")
	pkgs, err := npm.New().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %+v", pkgs)
	}
	paths := map[string]string{}
	for _, pkg := range pkgs {
		paths[pkg.Name] = pkg.Path
	}
	if paths["@demo/app"] != "packages/app" || paths["@demo/core"] != "packages/core" {
		t.Fatalf("unexpected paths: %+v", paths)
	}
}

func TestSetVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "npm-basic")
	adapter := npm.New()
	pkgs, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetVersion(root, pkgs[0], "1.3.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	updated, err := adapter.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if updated[0].Version != "1.3.0" {
		t.Fatalf("version not updated: %+v", updated[0])
	}
}

type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Run(_ context.Context, _ string, name string, args ...string) (execx.Result, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return execx.Result{}, nil
}

func TestPublishUsesChannelDistTag(t *testing.T) {
	run := &recordingRunner{}
	publisher := npm.New().Publisher()
	pkg := ecosystem.Package{Name: "demo", Path: "."}
	if err := publisher.Publish(context.Background(), run, t.TempDir(), pkg, "0.5.0-alpha.2"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(run.calls) != 1 || run.calls[0] != "npm publish --tag alpha" {
		t.Fatalf("calls = %v, want the alpha dist-tag", run.calls)
	}
	run.calls = nil
	if err := publisher.Publish(context.Background(), run, t.TempDir(), pkg, "0.5.0"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(run.calls) != 1 || run.calls[0] != "npm publish" {
		t.Fatalf("calls = %v, want a plain publish", run.calls)
	}
}
