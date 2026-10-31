package scaffold

import (
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

// buildPlan renders a plan with workflows enabled.
func buildPlan(t *testing.T, opts Options) *Plan {
	t.Helper()
	opts.Root = t.TempDir()
	opts.WithWorkflows = true
	opts.BaseBranch = "main"
	opts.ReleaseBranch = "release"
	if opts.Mode == "" {
		opts.Mode = config.ModePR
	}
	opts.TagPrefix = "v"
	opts.Remote = "origin"
	opts.ChangesDir = ".changes"
	if opts.QeggVersion == "" {
		opts.QeggVersion = "v0.7.0"
	}
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
		"  plan:",
		"  plan:\n    runs-on: ubuntu-latest",
		"    outputs:\n      phase: ${{ steps.plan.outputs.phase }}",
		"          qegg plan --json > \"$RUNNER_TEMP/plan.json\"",
		"  build-windows:",
		"  build-macos:",
		"  build-windows:\n    needs: plan\n    if: needs.plan.outputs.phase == 'publish'",
		"    runs-on: windows-latest",
		"    timeout-minutes: 30",
		"    runs-on: macos-latest",
		"    timeout-minutes: 45",
		"  version:",
		"  version:\n    needs:\n      - plan\n    runs-on: ubuntu-latest",
		"    if: needs.plan.outputs.phase == 'version'",
		"  publish:",
		"  publish:\n    needs:\n      - plan\n      - build-windows\n      - build-macos",
		"      - uses: actions/upload-artifact@v4",
		"          name: qegg-assets-windows",
		"            target/release/bundle/msi/*.msi",
		"      - uses: actions/download-artifact@v4",
		"          pattern: qegg-assets-*",
		"          path: .quailegg/assets/downloaded",
		"      - run: 'pnpm -C ui install --frozen-lockfile'",
		"        run: qegg release --yes --assets-dir .quailegg/assets/downloaded",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("release workflow is missing %q:\n%s", want, content)
		}
	}
	if got := strings.Count(content, "download-artifact"); got != 1 {
		t.Fatalf("only the publish job downloads artifacts, got %d occurrences:\n%s", got, content)
	}
	if got := strings.Count(content, "if: needs.plan.outputs.phase == 'publish'"); got != 3 {
		t.Fatalf("build jobs and the publish job must gate on the publish phase, got %d gates:\n%s", got, content)
	}
	if got := strings.Count(content, "        run: qegg release --yes\n"); got != 1 {
		t.Fatalf("the version job must call qegg release without assets, got %d:\n%s", got, content)
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
			YAML:           "- uses: actions/checkout@v4\n- uses: pnpm/action-setup@v4\n  with: { version: 11 }\n- run: pnpm build",
			Env:            map[string]string{"NODE_OPTIONS": "--max-old-space-size=4096"},
			Artifacts:      []string{"target/release/bundle/**/*"},
		}},
	})
	content := string(workflowFile(t, plan, "qegg-release.yml").Content)
	build := content[strings.Index(content, "  build-macos-arm:"):strings.Index(content, "  version:")]
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
			YAML:           "- uses: actions/setup-node@v4\n  with: { node-version: 22 }\n- run: echo release-env",
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
	if !strings.Contains(content, "  release:") || !strings.Contains(content, "run: qegg release --yes") {
		t.Fatalf("plain release job missing:\n%s", content)
	}
	for _, unexpected := range []string{
		"  plan:",
		"  version:",
		"  publish:",
		"    needs:",
		"needs.plan.outputs.phase",
		"download-artifact",
		"qegg plan --json",
	} {
		if strings.Contains(content, unexpected) {
			t.Fatalf("a workflow without build jobs must not contain %q:\n%s", unexpected, content)
		}
	}
}

func TestReleaseWorkflowGatesBuildsInCommitMode(t *testing.T) {
	plan := buildPlan(t, Options{
		Mode: config.ModeCommit,
		Workflow: config.Workflow{
			Mode:           config.WorkflowManaged,
			Runner:         "ubuntu-latest",
			TimeoutMinutes: 60,
		},
		Build: []config.BuildJob{{
			Name:           "windows",
			Runner:         "windows-latest",
			TimeoutMinutes: 30,
			Steps:          []string{"cargo tauri build"},
			Artifacts:      []string{"target/release/bundle/msi/*.msi"},
		}},
	})
	content := string(workflowFile(t, plan, "qegg-release.yml").Content)
	for _, want := range []string{
		"  plan:",
		"  build-windows:\n    needs: plan\n    if: needs.plan.outputs.phase != 'none'",
		"  release:\n    needs:\n      - plan\n      - build-windows",
		"    if: needs.plan.outputs.phase != 'none'",
		"      - uses: actions/download-artifact@v4",
		"        run: qegg release --yes --assets-dir .quailegg/assets/downloaded",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("commit-mode release workflow is missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "  version:") || strings.Contains(content, "  publish:") {
		t.Fatalf("commit mode must keep a single release job:\n%s", content)
	}
	if strings.Contains(content, "needs.plan.outputs.phase == 'publish'") {
		t.Fatalf("commit mode must not gate on the publish phase:\n%s", content)
	}
}

func TestReleaseWorkflowWithBuildJobsIsValidYAML(t *testing.T) {
	plan := buildPlan(t, Options{
		Workflow: config.Workflow{
			Mode:           config.WorkflowManaged,
			Runner:         "ubuntu-latest",
			TimeoutMinutes: 60,
		},
		Build: []config.BuildJob{
			{Name: "windows", Runner: "windows-latest", TimeoutMinutes: 30, Steps: []string{"echo build"}, Artifacts: []string{"dist/*.msi"}},
			{Name: "macos", Runner: "macos-latest", TimeoutMinutes: 45, Steps: []string{"echo build"}, Artifacts: []string{"dist/*.dmg"}},
		},
	})
	content := string(workflowFile(t, plan, "qegg-release.yml").Content)
	var doc struct {
		Jobs map[string]struct {
			Needs any              `yaml:"needs"`
			If    string           `yaml:"if"`
			Steps []map[string]any `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("release workflow with build jobs is invalid YAML: %v\n%s", err, content)
	}
	want := map[string]string{
		"plan":          "github.actor != 'github-actions[bot]'",
		"build-windows": "needs.plan.outputs.phase == 'publish'",
		"build-macos":   "needs.plan.outputs.phase == 'publish'",
		"version":       "needs.plan.outputs.phase == 'version'",
		"publish":       "needs.plan.outputs.phase == 'publish'",
	}
	if len(doc.Jobs) != len(want) {
		t.Fatalf("release workflow has %d jobs, want %d:\n%s", len(doc.Jobs), len(want), content)
	}
	for name, condition := range want {
		job, ok := doc.Jobs[name]
		if !ok {
			t.Fatalf("release workflow is missing job %q:\n%s", name, content)
		}
		if job.If != condition {
			t.Fatalf("job %q condition = %q, want %q", name, job.If, condition)
		}
		if len(job.Steps) < 2 {
			t.Fatalf("job %q has %d steps", name, len(job.Steps))
		}
	}
	if doc.Jobs["plan"].Needs != nil {
		t.Fatalf("the plan job must not need another job, got %v", doc.Jobs["plan"].Needs)
	}
}

func TestReleaseWorkflowGatesReuseQeggVersion(t *testing.T) {
	workflow := config.Workflow{Mode: config.WorkflowManaged, Runner: "ubuntu-latest", TimeoutMinutes: 60}
	build := []config.BuildJob{{Name: "windows", Runner: "windows-latest", TimeoutMinutes: 30, Steps: []string{"echo build"}, Artifacts: []string{"dist/*.msi"}}}

	pinned := buildPlan(t, Options{Workflow: workflow, Build: build})
	content := string(workflowFile(t, pinned, "qegg-release.yml").Content)
	if got := strings.Count(content, "cmd/qegg@v0.7.0"); got != 3 {
		t.Fatalf("plan, version and publish install qegg %d times, want 3:\n%s", got, content)
	}

	floating := buildPlan(t, Options{QeggVersion: "latest", Workflow: workflow, Build: build})
	content = string(workflowFile(t, floating, "qegg-release.yml").Content)
	if got := strings.Count(content, "cmd/qegg@latest"); got != 3 {
		t.Fatalf("the floating qegg version is used %d times, want 3:\n%s", got, content)
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

func TestDevelopWorkflowBuildsLocalQegg(t *testing.T) {
	plan := buildPlan(t, Options{Develop: true, QeggVersion: "latest", WithStatus: true})
	for _, name := range []string{"qegg-release.yml", "qegg-pr.yml", "qegg-status.yml"} {
		file := workflowFile(t, plan, name)
		if file == nil {
			t.Fatalf("missing %s", name)
		}
		content := string(file.Content)
		for _, want := range []string{
			`go build -o "$RUNNER_TEMP/bin/qegg" ./cmd/qegg`,
			"cmd/qegg@main",
			"shell: bash",
			"set -euo pipefail",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s is missing %q:\n%s", name, want, content)
			}
		}
		var doc map[string]any
		if err := yaml.Unmarshal(file.Content, &doc); err != nil {
			t.Fatalf("%s is not valid YAML: %v\n%s", name, err, content)
		}
	}
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "latest") {
			t.Fatalf("develop mode must not warn about a floating version: %v", plan.Warnings)
		}
	}
}
