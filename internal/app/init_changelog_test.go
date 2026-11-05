package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

// TestInitChangelogToggle pins that qegg init owns the changelog switch: the
// flag writes it, and a plain re-init keeps the stored value.
func TestInitChangelogToggle(t *testing.T) {
	dir := newPythonWorkspace(t, map[string]string{"app": "1.0.0", "core": "1.0.0"})
	ctx := context.Background()

	off := false
	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.11.0", Changelog: &off, Force: true,
	}); err != nil {
		t.Fatalf("Init disabled: %v", err)
	}
	text := mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(text, "changelog = false") {
		t.Fatalf("changelog = false was not written:\n%s", text)
	}

	on := true
	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.11.0", Changelog: &on, Force: true,
	}); err != nil {
		t.Fatalf("Init enabled: %v", err)
	}
	text = mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(text, "changelog = true") || strings.Contains(text, "changelog = false") {
		t.Fatalf("changelog = true was not written:\n%s", text)
	}

	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.11.0", Force: true,
	}); err != nil {
		t.Fatalf("Init without a policy: %v", err)
	}
	text = mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(text, "changelog = true") {
		t.Fatalf("a plain re-init dropped the stored switch:\n%s", text)
	}
}
