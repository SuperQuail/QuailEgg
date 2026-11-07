package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

// TestInitRendersConfiguredStageWorkflows pins that the check-only workflows
// read their own sections: a project prepares dependencies for the pull request
// job without touching the release job, and a re-init keeps that section.
func TestInitRendersConfiguredStageWorkflows(t *testing.T) {
	dir := newPythonWorkspace(t, map[string]string{"app": "1.0.0", "core": "1.0.0"})
	ctx := context.Background()
	opts := InitOptions{
		WithWorkflows: true, WithStatus: true, AddPackages: true,
		QeggVersion: "v0.12.0", Force: true,
	}
	if _, err := Init(ctx, dir, ui.Discard(), opts); err != nil {
		t.Fatalf("Init: %v", err)
	}

	configPath := filepath.Join(dir, ".quailegg.toml")
	text := mustReadFile(t, configPath)
	text = strings.TrimRight(text, "\n") + "\n\n[pr.workflow]\n" +
		"runner = \"ubuntu-24.04\"\n" +
		"timeout_minutes = 25\n" +
		"steps = [\"uv sync --locked\"]\n"
	if err := os.WriteFile(configPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, dir, ui.Discard(), opts); err != nil {
		t.Fatalf("re-init: %v", err)
	}

	if kept := mustReadFile(t, configPath); !strings.Contains(kept, "[pr.workflow]") {
		t.Fatalf("init dropped the pull request section:\n%s", kept)
	}
	pr := mustReadFile(t, filepath.Join(dir, ".github", "workflows", "qegg-pr.yml"))
	for _, want := range []string{
		"    runs-on: ubuntu-24.04",
		"    timeout-minutes: 25",
		"      - run: 'uv sync --locked'",
		"astral-sh/setup-uv",
		"run: qegg check --ci",
	} {
		if !strings.Contains(pr, want) {
			t.Fatalf("pull request workflow is missing %q:\n%s", want, pr)
		}
	}
	status := mustReadFile(t, filepath.Join(dir, ".github", "workflows", "qegg-status.yml"))
	if !strings.Contains(status, "    timeout-minutes: 15") {
		t.Fatalf("status workflow does not use the default stage:\n%s", status)
	}
}
