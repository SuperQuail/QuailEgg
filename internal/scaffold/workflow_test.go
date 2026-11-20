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
	if opts.ReleaseBranch == "" {
		opts.ReleaseBranch = "release"
	}
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

func TestPullRequestWorkflowRendersStage(t *testing.T) {
	plan := buildPlan(t, Options{
		PRWorkflow: config.StageWorkflow{
			Runner:         "windows-latest",
			TimeoutMinutes: 20,
			Steps:          []string{"uv sync --locked"},
			Env:            map[string]string{"UV_PYTHON": "3.14"},
		},
	})
	content := string(workflowFile(t, plan, "qegg-pr.yml").Content)
	for _, want := range []string{
		"    runs-on: windows-latest",
		"    timeout-minutes: 20",
		"    env:\n      UV_PYTHON: \"3.14\"",
		"      - run: 'uv sync --locked'",
		"      - name: Check\n        run: qegg check --ci",
		"      - name: Preview\n        shell: bash",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("pull request workflow is missing %q:\n%s", want, content)
		}
	}
	if strings.Index(content, "uv sync --locked") > strings.Index(content, "qegg check --ci") {
		t.Fatalf("the custom step must run before the check:\n%s", content)
	}
	var doc struct {
		Jobs map[string]struct {
			RunsOn string            `yaml:"runs-on"`
			Env    map[string]string `yaml:"env"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("pull request workflow is invalid YAML: %v\n%s", err, content)
	}
	if job := doc.Jobs["check"]; job.RunsOn != "windows-latest" || job.Env["UV_PYTHON"] != "3.14" {
		t.Fatalf("check job = %+v", job)
	}
}

func TestPullRequestWorkflowRendersYAMLSteps(t *testing.T) {
	plan := buildPlan(t, Options{
		PRWorkflow: config.StageWorkflow{YAML: "- uses: astral-sh/setup-uv@v5"},
	})
	content := string(workflowFile(t, plan, "qegg-pr.yml").Content)
	if !strings.Contains(content, "      - uses: astral-sh/setup-uv@v5") {
		t.Fatalf("pull request workflow is missing the yaml step:\n%s", content)
	}
	if strings.Contains(content, "[[") {
		t.Fatalf("workflow contains an unrendered action:\n%s", content)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("pull request workflow is invalid YAML: %v\n%s", err, content)
	}
}

// pullRequestEvent is the part of the pull_request payload the check job's
// condition reads.
type pullRequestEvent struct {
	Repository string
	HeadRepo   string
	HeadRef    string
}

// checkJobRuns reports whether the rendered job condition lets the check run
// for event. It understands the || chain of == / != comparisons between
// Actions context references and single quoted literals that the generated
// workflow contains; any other shape fails the test, so a condition this
// helper cannot reason about never passes it. It models exactly the three
// context paths the guard is allowed to read, so a condition that reads any
// other path (which this helper cannot validate) also fails instead of passing.
func checkJobRuns(t *testing.T, condition string, event pullRequestEvent) bool {
	t.Helper()
	references := map[string]string{
		"github.repository":                             event.Repository,
		"github.event.pull_request.head.repo.full_name": event.HeadRepo,
		"github.head_ref":                               event.HeadRef,
	}
	resolve := func(token string) string {
		if literal, quoted := strings.CutPrefix(token, "'"); quoted {
			if len(literal) == 0 || !strings.HasSuffix(literal, "'") {
				t.Fatalf("condition %q has an unterminated string literal", condition)
			}
			return strings.ReplaceAll(literal[:len(literal)-1], "''", "'")
		}
		value, ok := references[token]
		if !ok {
			t.Fatalf("condition %q reads %q, which this test does not model", condition, token)
		}
		return value
	}
	// The guard runs the check when any clause of the || chain holds.
	runs := false
	for _, clause := range strings.Split(condition, " || ") {
		left, operator, right := cutComparison(t, condition, clause)
		equal := resolve(left) == resolve(right)
		if (operator == "==") == equal {
			runs = true
		}
	}
	return runs
}

// cutComparison splits one comparison clause into its operands and operator,
// so the test only accepts the operators the template is expected to render.
func cutComparison(t *testing.T, condition, clause string) (left, operator, right string) {
	t.Helper()
	if left, right, ok := strings.Cut(clause, " == "); ok {
		return left, "==", right
	}
	if left, right, ok := strings.Cut(clause, " != "); ok {
		return left, "!=", right
	}
	t.Fatalf("condition clause %q is not an == or != comparison", clause)
	return "", "", ""
}

// checkJobCondition renders the pull request workflow and returns the guard of
// its check job, so a test reads exactly what GitHub would evaluate.
func checkJobCondition(t *testing.T, opts Options) string {
	t.Helper()
	content := workflowFile(t, buildPlan(t, opts), "qegg-pr.yml")
	if content == nil {
		t.Fatal("the pull request workflow was not planned")
	}
	return checkJobIf(t, content.Content)
}

// checkJobIf returns the guard of the check job in a rendered workflow.
func checkJobIf(t *testing.T, content []byte) string {
	t.Helper()
	var doc struct {
		Jobs map[string]struct {
			If    string           `yaml:"if"`
			Steps []map[string]any `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("pull request workflow is invalid YAML: %v\n%s", err, content)
	}
	job, ok := doc.Jobs["check"]
	if !ok {
		t.Fatalf("pull request workflow has no check job:\n%s", content)
	}
	if len(job.Steps) == 0 {
		t.Fatalf("the check job has no steps:\n%s", content)
	}
	return job.If
}

// TestPullRequestWorkflowSkipsOnlyTheManagedReleaseBranch pins the job guard:
// only a pull request from the configured release branch of this repository
// skips the check, so a fork reusing the branch name still gets checked, and
// so does a pull request whose head repository the event does not report.
func TestPullRequestWorkflowSkipsOnlyTheManagedReleaseBranch(t *testing.T) {
	const repo = "SuperQuail/QuailEgg"
	inRepo := func(headRef string) pullRequestEvent {
		return pullRequestEvent{Repository: repo, HeadRepo: repo, HeadRef: headRef}
	}
	fromFork := func(headRef string) pullRequestEvent {
		return pullRequestEvent{Repository: repo, HeadRepo: "someone/QuailEgg", HeadRef: headRef}
	}
	defaults := config.Default()
	cases := []struct {
		name    string
		release string
		event   pullRequestEvent
		runs    bool
	}{
		{
			name:    "managed release branch of this repository",
			release: "release-1.x",
			event:   inRepo("release-1.x"),
			runs:    false,
		},
		{
			name:    "ordinary branch of this repository",
			release: "release-1.x",
			event:   inRepo("fix/record-checks"),
			runs:    true,
		},
		{
			name:    "fork branch named like the release branch",
			release: "release-1.x",
			event:   fromFork("release-1.x"),
			runs:    true,
		},
		{
			name:    "configured release branch",
			release: defaults.Release.Branch,
			event:   inRepo(defaults.Release.Branch),
			runs:    false,
		},
		{
			name:    "fork of another repository owned by the same account",
			release: "release-1.x",
			event:   pullRequestEvent{Repository: repo, HeadRepo: "SuperQuail/QuailEgg-fork", HeadRef: "release-1.x"},
			runs:    true,
		},
		{
			name:    "fork whose head repository the event does not report",
			release: "release-1.x",
			event:   pullRequestEvent{Repository: repo, HeadRepo: "", HeadRef: "release-1.x"},
			runs:    true,
		},
		{
			name:    "custom release branch name",
			release: "cut/next",
			event:   inRepo("cut/next"),
			runs:    false,
		},
		{
			name:    "custom release branch name on a fork branch of the same name",
			release: "cut/next",
			event:   fromFork("cut/next"),
			runs:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			condition := checkJobCondition(t, Options{ReleaseBranch: tc.release})
			want := "github.event.pull_request.head.repo.full_name != github.repository" +
				" || github.head_ref != '" + tc.release + "'"
			if condition != want {
				t.Fatalf("check job condition = %q, want %q", condition, want)
			}
			if got := checkJobRuns(t, condition, tc.event); got != tc.runs {
				t.Fatalf("check job runs = %v for %+v, want %v", got, tc.event, tc.runs)
			}
		})
	}
}

// TestPullRequestWorkflowQuotesReleaseBranch keeps the configured branch name
// from breaking the generated workflow: it may contain characters a YAML
// scalar or an Actions expression would otherwise interpret.
func TestPullRequestWorkflowQuotesReleaseBranch(t *testing.T) {
	const repo = "SuperQuail/QuailEgg"
	cases := []struct {
		name   string
		branch string
		want   string
	}{
		{name: "apostrophe", branch: "jane's-release", want: "'jane''s-release'"},
		{name: "space and hash", branch: "release #1", want: "'release #1'"},
		{name: "double quote", branch: `rel"ease`, want: `'rel"ease'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			condition := checkJobCondition(t, Options{ReleaseBranch: tc.branch})
			want := "github.event.pull_request.head.repo.full_name != github.repository" +
				" || github.head_ref != " + tc.want
			if condition != want {
				t.Fatalf("check job condition = %q, want %q", condition, want)
			}
			managed := pullRequestEvent{Repository: repo, HeadRepo: repo, HeadRef: tc.branch}
			if checkJobRuns(t, condition, managed) {
				t.Fatalf("check job runs for the managed release branch %q", tc.branch)
			}
			fork := pullRequestEvent{Repository: repo, HeadRepo: "someone/QuailEgg", HeadRef: tc.branch}
			if !checkJobRuns(t, condition, fork) {
				t.Fatalf("check job skips the fork branch %q", tc.branch)
			}
		})
	}
}

// TestPullRequestWorkflowFallsBackToTheDefaultReleaseBranch covers a caller
// that leaves the release branch empty: rendering normalises it once, so both
// the pull request guard and the release workflow name the built-in default
// instead of an empty branch.
func TestPullRequestWorkflowFallsBackToTheDefaultReleaseBranch(t *testing.T) {
	defaults := config.Default()
	data := newView(Options{ReleaseBranch: "", BaseBranch: "main"})
	pr, err := assets.render("github/workflows/qegg-pr.yml.tmpl", data)
	if err != nil {
		t.Fatalf("render the pull request workflow: %v", err)
	}
	condition := checkJobIf(t, pr)
	want := "github.event.pull_request.head.repo.full_name != github.repository" +
		" || github.head_ref != '" + defaults.Release.Branch + "'"
	if condition != want {
		t.Fatalf("check job condition = %q, want %q", condition, want)
	}
	const repo = "SuperQuail/QuailEgg"
	managed := pullRequestEvent{Repository: repo, HeadRepo: repo, HeadRef: defaults.Release.Branch}
	if checkJobRuns(t, condition, managed) {
		t.Fatalf("check job runs for the default release branch %q", defaults.Release.Branch)
	}
	// The normalised name is what every template receives, so a branch filter
	// or guard can never render an empty branch.
	if data.ReleaseBranch != defaults.Release.Branch {
		t.Fatalf("view release branch = %q, want the default %q", data.ReleaseBranch, defaults.Release.Branch)
	}
}

func TestStatusWorkflowRendersStage(t *testing.T) {
	plan := buildPlan(t, Options{
		WithStatus: true,
		StatusWorkflow: config.StageWorkflow{
			TimeoutMinutes: 5,
			Steps:          []string{"echo status"},
		},
	})
	content := string(workflowFile(t, plan, "qegg-status.yml").Content)
	for _, want := range []string{
		"    timeout-minutes: 5",
		"      - run: 'echo status'",
		"      - run: qegg status --json",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("status workflow is missing %q:\n%s", want, content)
		}
	}
	if strings.Index(content, "echo status") > strings.Index(content, "qegg status --json") {
		t.Fatalf("the custom step must run before the status command:\n%s", content)
	}
}

func TestCheckWorkflowsUseDefaultStage(t *testing.T) {
	plan := buildPlan(t, Options{WithStatus: true})
	for _, name := range []string{"qegg-pr.yml", "qegg-status.yml"} {
		content := string(workflowFile(t, plan, name).Content)
		if !strings.Contains(content, "    runs-on: ubuntu-latest") {
			t.Errorf("%s does not use the default runner:\n%s", name, content)
		}
		if !strings.Contains(content, "    timeout-minutes: 15") {
			t.Errorf("%s does not use the default timeout:\n%s", name, content)
		}
	}
}
