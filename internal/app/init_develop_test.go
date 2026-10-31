package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInitDevelopToggle(t *testing.T) {
	dir := newPythonWorkspace(t, map[string]string{"app": "1.0.0", "core": "1.0.0"})
	ctx := context.Background()
	on := true
	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.9.0", Develop: &on, Force: true,
	}); err != nil {
		t.Fatalf("Init develop: %v", err)
	}
	configText := mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(configText, "develop = true") {
		t.Fatalf("develop was not written:\n%s", configText)
	}
	release := mustReadFile(t, filepath.Join(dir, ".github", "workflows", "qegg-release.yml"))
	if !strings.Contains(release, "go build -o \"$RUNNER_TEMP/bin/qegg\" ./cmd/qegg") {
		t.Fatalf("the workflow does not build the checkout:\n%s", release)
	}

	off := false
	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.9.0", Develop: &off, Force: true,
	}); err != nil {
		t.Fatalf("Init pinned: %v", err)
	}
	configText = mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if strings.Contains(configText, "develop = true") {
		t.Fatalf("develop was not cleared:\n%s", configText)
	}
	release = mustReadFile(t, filepath.Join(dir, ".github", "workflows", "qegg-release.yml"))
	if strings.Contains(release, "go build -o \"$RUNNER_TEMP/bin/qegg\" ./cmd/qegg") {
		t.Fatalf("the pinned workflow still builds the checkout:\n%s", release)
	}
	if !strings.Contains(release, "cmd/qegg@v0.9.0") {
		t.Fatalf("the pinned workflow lost its version:\n%s", release)
	}
}
