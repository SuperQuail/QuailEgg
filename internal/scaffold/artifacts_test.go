package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

const (
	configPlanPath          = ".quailegg.toml"
	changesReadmePlanPath   = ".changes/README.md"
	hookPlanPath            = ".git/hooks/pre-commit"
	releaseWorkflowPlanPath = ".github/workflows/qegg-release.yml"
	prWorkflowPlanPath      = ".github/workflows/qegg-pr.yml"
	statusWorkflowPlanPath  = ".github/workflows/qegg-status.yml"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Root:          t.TempDir(),
		BaseBranch:    "main",
		ReleaseBranch: "release",
		Mode:          config.ModePR,
		TagPrefix:     "v",
		Remote:        "origin",
		ChangesDir:    ".changes",
		QeggVersion:   "latest",
		Ecosystem:     "npm",
		WithHook:      true,
		WithWorkflows: true,
		WithStatus:    true,
		Packages: map[string]config.Package{
			"demo": {Path: ".", Resolver: config.ResolverNPM},
		},
	}
}

// withEcosystem points the options at one ecosystem ("" means none) so the
// workflow derivation sees a matching package.
func withEcosystem(opts Options, id string) Options {
	opts.Ecosystem = id
	opts.Registries = nil
	if id == "" {
		opts.Packages = map[string]config.Package{}
		return opts
	}
	opts.Packages = map[string]config.Package{
		"demo": {Path: ".", Resolver: id},
	}
	return opts
}

func mustBuild(t *testing.T, opts Options) *Plan {
	t.Helper()
	plan, err := Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return plan
}

func planFileByPath(t *testing.T, plan *Plan, rel string) *File {
	t.Helper()
	for _, file := range plan.Files {
		if fsx.Rel(plan.Root, file.Path) == rel {
			return file
		}
	}
	t.Fatalf("plan has no file %s", rel)
	return nil
}

func TestArtifactMatrix(t *testing.T) {
	ecosystems := []string{"", "npm", "python", "rust", "go"}
	branches := []string{"main", "release/1.x"}
	versions := []string{"latest", "v0.1.0"}
	gates := []struct {
		name      string
		hook      bool
		workflows bool
		status    bool
	}{
		{name: "all", hook: true, workflows: true, status: true},
		{name: "no status workflow", hook: true, workflows: true, status: false},
		{name: "no workflows", hook: true, workflows: false, status: false},
		{name: "config only", hook: false, workflows: false, status: false},
	}
	for _, eco := range ecosystems {
		for _, branch := range branches {
			for _, version := range versions {
				for _, gate := range gates {
					gate := gate
					name := fmt.Sprintf("eco=%q/branch=%s/version=%s/%s", eco, branch, version, gate.name)
					t.Run(name, func(t *testing.T) {
						opts := withEcosystem(testOptions(t), eco)
						opts.BaseBranch = branch
						opts.QeggVersion = version
						opts.WithHook = gate.hook
						opts.WithWorkflows = gate.workflows
						opts.WithStatus = gate.status
						plan := mustBuild(t, opts)

						want := []string{configPlanPath, changesReadmePlanPath}
						if gate.hook {
							want = append(want, hookPlanPath)
						}
						if gate.workflows {
							want = append(want, releaseWorkflowPlanPath, prWorkflowPlanPath)
						}
						if gate.workflows && gate.status {
							want = append(want, statusWorkflowPlanPath)
						}
						want = append(want, gitignoreName)
						if len(plan.Files) != len(want) {
							t.Fatalf("plan has %d files, want %d", len(plan.Files), len(want))
						}
						for i, rel := range want {
							if got := fsx.Rel(plan.Root, plan.Files[i].Path); got != rel {
								t.Fatalf("plan[%d] = %s, want %s", i, got, rel)
							}
						}
						for _, file := range plan.Files {
							rel := fsx.Rel(plan.Root, file.Path)
							if file.Action != ActionCreate {
								t.Errorf("%s: action = %s, want create", rel, file.Action)
							}
							text := string(file.Content)
							if strings.Contains(text, openAction) || strings.Contains(text, closeAction) {
								t.Errorf("%s: template delimiters left in the output", rel)
							}
							if strings.Contains(text, "\r") {
								t.Errorf("%s: output contains CRLF", rel)
							}
							if strings.HasPrefix(rel, "..") {
								t.Errorf("%s: destination escapes the repository root", rel)
							}
							if rel == hookPlanPath || strings.HasSuffix(rel, ".yml") {
								if !strings.HasSuffix(text, "\n") {
									t.Errorf("%s: output does not end with a newline", rel)
								}
							}
							if strings.HasSuffix(rel, ".yml") && strings.Contains(text, "\n\n\n") {
								t.Errorf("%s: output contains a doubled blank line", rel)
							}
						}
						if gate.workflows {
							for _, rel := range []string{releaseWorkflowPlanPath, prWorkflowPlanPath} {
								if !strings.Contains(string(planFileByPath(t, plan, rel).Content), branch) {
									t.Errorf("%s: base branch %q is missing", rel, branch)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestWorkflowAssetsAreValidYAML(t *testing.T) {
	for _, eco := range []string{"", "npm", "python", "rust", "go"} {
		opts := withEcosystem(testOptions(t), eco)
		plan := mustBuild(t, opts)
		for _, rel := range []string{releaseWorkflowPlanPath, prWorkflowPlanPath, statusWorkflowPlanPath} {
			content := string(planFileByPath(t, plan, rel).Content)
			var doc struct {
				Jobs map[string]struct {
					Steps []map[string]any `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
				t.Fatalf("%s (ecosystem %q): invalid YAML: %v", rel, eco, err)
			}
			if len(doc.Jobs) == 0 {
				t.Fatalf("%s (ecosystem %q): no jobs", rel, eco)
			}
			for name, job := range doc.Jobs {
				if len(job.Steps) < 2 {
					t.Errorf("%s: job %q has %d steps", rel, name, len(job.Steps))
				}
			}
			if !strings.Contains(content, "\non:\n") {
				t.Errorf("%s: missing an on: trigger block", rel)
			}
		}
	}
}

func TestGitHubExpressionsArePreserved(t *testing.T) {
	plan := mustBuild(t, testOptions(t))
	release := string(planFileByPath(t, plan, releaseWorkflowPlanPath).Content)
	if !strings.Contains(release, "${{ secrets.GITHUB_TOKEN }}") {
		t.Errorf("release workflow is missing the GITHUB_TOKEN expression")
	}
	pr := string(planFileByPath(t, plan, prWorkflowPlanPath).Content)
	if !strings.Contains(pr, "${{ github.event.pull_request.number }}") {
		t.Errorf("pull request workflow is missing the pull request number expression")
	}
}

func TestEcosystemSetupIsWiredIntoReleaseWorkflow(t *testing.T) {
	for _, spec := range registry.Specs() {
		if strings.TrimSpace(spec.WorkflowSetup) == "" {
			// Every generated workflow already installs Go to build qegg, so
			// the Go ecosystem needs no setup of its own.
			continue
		}
		var steps []map[string]any
		if err := yaml.Unmarshal([]byte(spec.WorkflowSetup), &steps); err != nil {
			t.Fatalf("%s: workflow setup is not a YAML sequence: %v", spec.ID, err)
		}
		if len(steps) == 0 {
			t.Fatalf("%s: workflow setup has no steps", spec.ID)
		}
		uses, _ := steps[0]["uses"].(string)
		if uses == "" {
			t.Fatalf("%s: workflow setup step has no uses", spec.ID)
		}
		opts := withEcosystem(testOptions(t), spec.ID)
		release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
		if !strings.Contains(release, "      - uses: "+uses) {
			t.Errorf("%s: setup step %q is missing from the release workflow", spec.ID, uses)
		}
	}

	opts := withEcosystem(testOptions(t), "")
	release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
	for _, unexpected := range []string{"actions/setup-node", "astral-sh/setup-uv", "dtolnay/rust-toolchain"} {
		if strings.Contains(release, unexpected) {
			t.Errorf("release workflow without an ecosystem contains %q", unexpected)
		}
	}
}

func TestEcosystemEnvIsWiredIntoReleaseWorkflow(t *testing.T) {
	envKeys := map[string][]string{}
	for _, spec := range registry.Specs() {
		if strings.TrimSpace(spec.WorkflowEnv) == "" {
			continue
		}
		var env map[string]string
		if err := yaml.Unmarshal([]byte(spec.WorkflowEnv), &env); err != nil {
			t.Fatalf("%s: workflow env is not a YAML mapping: %v", spec.ID, err)
		}
		if len(env) == 0 {
			t.Fatalf("%s: workflow env is empty", spec.ID)
		}
		keys := make([]string, 0, len(env))
		for key := range env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		envKeys[spec.ID] = keys

		opts := withEcosystem(testOptions(t), spec.ID)
		release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
		for _, key := range keys {
			if !strings.Contains(release, "          "+key+": ${{ secrets.") {
				t.Errorf("%s: release workflow is missing the %s environment variable", spec.ID, key)
			}
		}
	}

	release := string(planFileByPath(t, mustBuild(t, withEcosystem(testOptions(t), "")), releaseWorkflowPlanPath).Content)
	for id, keys := range envKeys {
		for _, key := range keys {
			if strings.Contains(release, key+":") {
				t.Errorf("release workflow without an ecosystem contains %s (%s)", key, id)
			}
		}
	}
}

func TestMultiplePublishTargetsContributeFragments(t *testing.T) {
	opts := testOptions(t)
	opts.Ecosystem = ""
	opts.Registries = []string{"npm", "pypi", "crates"}
	opts.Packages = map[string]config.Package{}
	release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
	for _, want := range []string{
		"      - uses: actions/setup-node@v4",
		"      - uses: astral-sh/setup-uv@v5",
		"      - uses: dtolnay/rust-toolchain@stable",
		"          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}",
		"          UV_PUBLISH_TOKEN: ${{ secrets.UV_PUBLISH_TOKEN }}",
		"          CARGO_REGISTRY_TOKEN: ${{ secrets.CARGO_REGISTRY_TOKEN }}",
	} {
		if !strings.Contains(release, want) {
			t.Errorf("release workflow for multiple publish targets is missing %q", want)
		}
	}
}

func TestRegistryAliasesMapToEcosystems(t *testing.T) {
	cases := []struct {
		target string
		setup  string
		env    string
	}{
		{target: "pypi", setup: "astral-sh/setup-uv@v5", env: "UV_PUBLISH_TOKEN:"},
		{target: "crates", setup: "dtolnay/rust-toolchain@stable", env: "CARGO_REGISTRY_TOKEN:"},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			opts := withEcosystem(testOptions(t), "")
			opts.Registries = []string{tc.target}
			release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
			for _, want := range []string{tc.setup, tc.env} {
				if !strings.Contains(release, want) {
					t.Errorf("publish target %q is missing %q", tc.target, want)
				}
			}
		})
	}
}

func TestGoWorkflowNeedsNoEcosystemFragments(t *testing.T) {
	opts := withEcosystem(testOptions(t), "go")
	release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
	for _, unexpected := range []string{
		"actions/setup-node", "astral-sh/setup-uv", "dtolnay/rust-toolchain",
		"NODE_AUTH_TOKEN", "UV_PUBLISH_TOKEN", "CARGO_REGISTRY_TOKEN",
	} {
		if strings.Contains(release, unexpected) {
			t.Errorf("go release workflow contains %q", unexpected)
		}
	}
	if got := strings.Count(release, "actions/setup-go"); got != 1 {
		t.Errorf("go release workflow has %d setup-go steps, want the single base step", got)
	}
}

func TestReleaseWorkflowConfiguresGitIdentity(t *testing.T) {
	release := string(planFileByPath(t, mustBuild(t, testOptions(t)), releaseWorkflowPlanPath).Content)
	for _, want := range []string{
		`git config user.name "github-actions[bot]"`,
		`git config user.email "41898282+github-actions[bot]@users.noreply.github.com"`,
	} {
		if !strings.Contains(release, want) {
			t.Errorf("release workflow is missing %q", want)
		}
	}
}

func TestQeggVersionPinAndWarning(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	opts := testOptions(t)
	opts.QeggVersion = "v0.1.0"
	release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
	if !strings.Contains(release, "cmd/qegg@v0.1.0") {
		t.Errorf("release workflow does not pin qegg v0.1.0: %s", release)
	}

	latest := testOptions(t)
	latest.QeggVersion = "latest"
	found := false
	for _, warning := range mustBuild(t, latest).Warnings {
		if warning == i18n.T("init.warn_latest_qegg") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the @latest warning for a floating qegg pin")
	}
}

func TestHookAsset(t *testing.T) {
	opts := testOptions(t)
	file := planFileByPath(t, mustBuild(t, opts), hookPlanPath)
	if file.Perm != 0o755 {
		t.Errorf("hook permission = %v, want 0755", file.Perm)
	}
	script := string(file.Content)
	if !strings.HasPrefix(script, "#!/bin/sh\n") {
		t.Errorf("hook does not start with a POSIX shebang:\n%s", script)
	}
	if !strings.Contains(script, "exec qegg hook pre-commit") {
		t.Errorf("hook does not call qegg:\n%s", script)
	}
	if strings.Contains(script, openAction) || strings.Contains(script, closeAction) {
		t.Errorf("hook contains template delimiters:\n%s", script)
	}
	for _, lang := range i18n.Supported() {
		if err := i18n.Init(lang); err != nil {
			t.Fatalf("i18n.Init(%s): %v", lang, err)
		}
		out, err := assets.render("git/hooks/pre-commit.sh.tmpl", newView(opts))
		if err != nil {
			t.Fatalf("%s: render hook: %v", lang, err)
		}
		want := shquote(i18n.T("scaffold.hook_note"))
		if !strings.Contains(string(out), want) {
			t.Errorf("%s: hook does not embed %q", lang, want)
		}
	}
	if err := i18n.Init("en"); err != nil {
		t.Fatal(err)
	}
}

func TestExistingFilesAreSkippedUnlessForced(t *testing.T) {
	opts := testOptions(t)
	target := filepath.Join(opts.Root, filepath.FromSlash(releaseWorkflowPlanPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("name: local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	file := planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath)
	if file.Action != ActionSkip {
		t.Errorf("existing file action = %s, want skip", file.Action)
	}

	opts.Force = true
	file = planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath)
	if file.Action != ActionUpdate {
		t.Errorf("forced file action = %s, want update", file.Action)
	}
}

func TestAssetsAreCoveredByCatalog(t *testing.T) {
	referenced := map[string]bool{}
	for _, item := range artifacts {
		if item.ID == "" || item.Dest == "" {
			t.Errorf("artifact %+v is incomplete", item)
		}
		if referenced[item.Source] {
			t.Errorf("artifact source %s is listed twice", item.Source)
		}
		referenced[item.Source] = true
		if _, err := fs.Stat(assetFS, "assets/"+item.Source); err != nil {
			t.Errorf("catalog source %s: %v", item.Source, err)
		}
	}
	err := fs.WalkDir(assetFS, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, "assets/")
		if !referenced[rel] {
			t.Errorf("asset %s is not in the catalog", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoEmbeddedArtifactsInSources(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	markers := []string{"runs-on:", "#!/bin/sh", "actions/checkout@"}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range markers {
			if strings.Contains(string(data), marker) {
				t.Errorf("%s embeds %q; keep generated content under assets/", entry.Name(), marker)
			}
		}
	}
}

func TestReleaseWorkflowConfiguresEnvironment(t *testing.T) {
	opts := testOptions(t)
	opts.Environment = "pypi"
	release := string(planFileByPath(t, mustBuild(t, opts), releaseWorkflowPlanPath).Content)
	if !strings.Contains(release, "    environment: pypi") {
		t.Fatalf("release workflow does not configure the environment:\n%s", release)
	}
}

func TestPRWorkflowSkipsEmptyPreview(t *testing.T) {
	pr := string(planFileByPath(t, mustBuild(t, testOptions(t)), prWorkflowPlanPath).Content)
	for _, want := range []string{"if [ -s plan.md ]; then", "no pending release"} {
		if !strings.Contains(pr, want) {
			t.Errorf("PR workflow is missing %q", want)
		}
	}
}
