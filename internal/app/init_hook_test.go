package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

// TestInitHookVerifyToggle pins that qegg init owns the commit-time gate: the
// flag writes the switch, and a plain re-init keeps the stored value.
func TestInitHookVerifyToggle(t *testing.T) {
	dir := newPythonWorkspace(t, map[string]string{"app": "1.0.0", "core": "1.0.0"})
	ctx := context.Background()

	off := false
	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.11.0", Hook: &off, Force: true,
	}); err != nil {
		t.Fatalf("Init disabled: %v", err)
	}
	text := mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(text, "hook = false") {
		t.Fatalf("hook = false was not written:\n%s", text)
	}

	on := true
	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.11.0", Hook: &on, Force: true,
	}); err != nil {
		t.Fatalf("Init enabled: %v", err)
	}
	text = mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(text, "hook = true") || strings.Contains(text, "hook = false") {
		t.Fatalf("hook = true was not written:\n%s", text)
	}

	if _, err := Init(ctx, dir, ui.Discard(), InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.11.0", Force: true,
	}); err != nil {
		t.Fatalf("Init without a policy: %v", err)
	}
	text = mustReadFile(t, filepath.Join(dir, ".quailegg.toml"))
	if !strings.Contains(text, "hook = true") {
		t.Fatalf("a plain re-init dropped the stored policy:\n%s", text)
	}
}
