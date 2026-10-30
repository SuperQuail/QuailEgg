package scaffold

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

// buildPlan renders a plan with workflows enabled.
func buildPlan(t *testing.T, opts Options) *Plan {
	t.Helper()
	opts.Root = t.TempDir()
	opts.WithWorkflows = true
	opts.BaseBranch = "main"
	opts.ReleaseBranch = "release"
	opts.Mode = config.ModePR
	opts.TagPrefix = "v"
	opts.Remote = "origin"
	opts.ChangesDir = ".changes"
	opts.QeggVersion = "v0.7.0"
	opts.Scope = []string{"internal"}
	opts.VerifyCommands = []string{"go test ./..."}
	if opts.Workflow.Mode == "" {
		opts.Workflow = config.Workflow{Mode: config.WorkflowManaged, Runner: config.DefaultWorkflowRunner, TimeoutMinutes: 60}
	}
	plan, err := Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return plan
}

func workflowFile(t *testing.T, plan *Plan, name string) *File {
	t.Helper()
	for _, file := range plan.Files {
		if filepath.Base(file.Path) == name {
			return file
		}
	}
	return nil
}

func TestReleaseWorkflowRendersBuildJobs(t *testing.T) {
	plan := buildPlan(t, Options{
		Workflow: config.Workflow{
			Mode:           config.WorkflowManaged,
			Runner:         "ubuntu-latest",
			TimeoutMinutes: 60,
			Steps:          []string{"pnpm -C ui install --frozen-lockfile"},
		},
		Build: []config.BuildJob{
			{
				Name:           "windows",
				Runner:         "windows-latest",
				TimeoutMinutes: 30,
				Steps:          []string{"pnpm -C ui build", "cargo tauri build"},
				Artifacts:      []string{"target/release/bundle/msi/*.msi"},
			},
			{
				Name:           "macos",
				Runner:         "macos-latest",
				TimeoutMinutes: 45,
				Steps:          []string{"cargo tauri build"},
				Artifacts:      []string{"target/release/bundle/dmg/*.dmg"},
			},
		},
	})
	file := workflowFile(t, plan, "qegg-release.yml")
	if file == nil {
		t.Fatal("the release workflow was not planned")
	}
	content := string(file.Content)
	for _, want := range []string{
		"  build-windows:",
		"  build-macos:",
		"    runs-on: windows-latest",
		"    timeout-minutes: 30",
		"    runs-on: macos-latest",
		"    timeout-minutes: 45",
		"    needs:\n      - build-windows\n      - build-macos",
		"      - uses: actions/upload-artifact@v4",
		"          name: qegg-assets-windows",
		"            target/release/bundle/msi/*.msi",
		"      - uses: actions/download-artifact@v4",
		"          pattern: qegg-assets-*",
		"          path: .quailegg/assets/downloaded",
		"      - run: 'pnpm -C ui install --frozen-lockfile'",
		"      - name: Release\n        run: qegg release --yes --assets-dir .quailegg/assets/downloaded",
		"    runs-on: ubuntu-latest",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("release workflow is missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "[[- ") || strings.Contains(content, "[[ ") {
		t.Fatal("workflow contains an unrendered template action")
	}
}

func TestReleaseWorkflowWithoutBuildJobs(t *testing.T) {
	plan := buildPlan(t, Options{})
	content := string(workflowFile(t, plan, "qegg-release.yml").Content)
	if !strings.Contains(content, "run: qegg release --yes") {
		t.Fatalf("plain release step missing:\n%s", content)
	}
	if strings.Contains(content, "download-artifact") || strings.Contains(content, "needs:") {
		t.Fatalf("a workflow without build jobs must not depend on artifacts:\n%s", content)
	}
}

func TestExternalWorkflowSkipsReleaseWorkflow(t *testing.T) {
	plan := buildPlan(t, Options{Workflow: config.Workflow{Mode: config.WorkflowExternal, Runner: config.DefaultWorkflowRunner, TimeoutMinutes: 60}})
	if file := workflowFile(t, plan, "qegg-release.yml"); file != nil {
		t.Fatal("external mode must not generate the release workflow")
	}
	if file := workflowFile(t, plan, "qegg-pr.yml"); file == nil {
		t.Fatal("the pull request workflow must still be generated")
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "external") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an external workflow warning, got %v", plan.Warnings)
	}
}
