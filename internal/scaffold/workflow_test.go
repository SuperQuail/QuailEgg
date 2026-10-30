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

func TestReleaseWorkflowRendersYAMLSteps(t *testing.T) {
	plan := buildPlan(t, Options{
		Workflow: config.Workflow{
			Mode:           config.WorkflowManaged,
			Runner:         "ubuntu-latest",
			TimeoutMinutes: 60,
			Env:            map[string]string{"CACHE_DIR": ".cache"},
		},
		Build: []config.BuildJob{{
			Name:           "macos-arm",
			Runner:         "macos-latest",
			TimeoutMinutes: 45,
			YAML:            "- uses: actions/checkout@v4\n- uses: pnpm/action-setup@v4\n  with: { version: 11 }\n- run: pnpm build",
			Env:             map[string]string{"NODE_OPTIONS": "--max-old-space-size=4096"},
			Artifacts:       []string{"target/release/bundle/**/*"},
		}},
	})
	content := string(workflowFile(t, plan, "qegg-release.yml").Content)
	build := content[strings.Index(content, "  build-macos-arm:"):strings.Index(content, "  release:")]
	for _, want := range []string{
		"    env:\n      NODE_OPTIONS: \"--max-old-space-size=4096\"",
		"      - uses: actions/checkout@v4",
		"      - uses: pnpm/action-setup@v4",
		"        with: { version: 11 }",
		"      - run: pnpm build",
		"      - uses: actions/upload-artifact@v4",
		"          name: qegg-assets-macos-arm",
	} {
		if !strings.Contains(build, want) {
			t.Fatalf("build job is missing %q:\n%s", want, build)
		}
	}
	if strings.Contains(build, "actions/setup-go") || strings.Contains(build, "dtolnay") {
		t.Fatalf("yaml mode must not add built-in setup steps:\n%s", build)
	}
	if !strings.Contains(content, "    env:\n      CACHE_DIR: \".cache\"") {
		t.Fatalf("release job env missing:\n%s", content)
	}
}

func TestReleaseWorkflowYAMLReleaseSteps(t *testing.T) {
	plan := buildPlan(t, Options{
		Workflow: config.Workflow{
			Mode:           config.WorkflowManaged,
			Runner:         "ubuntu-latest",
			TimeoutMinutes: 60,
			YAML:            "- uses: actions/setup-node@v4\n  with: { node-version: 22 }\n- run: echo release-env",
		},
	})
	content := string(workflowFile(t, plan, "qegg-release.yml").Content)
	for _, want := range []string{
		"      - uses: actions/setup-node@v4",
		"        with: { node-version: 22 }",
		"      - run: echo release-env",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("release job is missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "[[") {
		t.Fatalf("workflow contains an unrendered action:\n%s", content)
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
