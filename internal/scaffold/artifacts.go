package scaffold

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// artifact describes one generated file backed by an embedded asset.
type artifact struct {
	ID      string
	Source  string // path under assets/
	Dest    string // path relative to the repository root, rendered as a template
	Perm    fs.FileMode
	Reason  string
	Enabled func(Options) bool
}

// artifacts lists the generated files in the order qegg init reports them.
var artifacts = []artifact{
	{
		ID:      "hook",
		Source:  "git/hooks/pre-commit.sh.tmpl",
		Dest:    ".git/hooks/pre-commit",
		Perm:    0o755,
		Reason:  "pre-commit hook",
		Enabled: func(opts Options) bool { return opts.WithHook },
	},
	{
		ID:     "workflow-release",
		Source: "github/workflows/qegg-release.yml.tmpl",
		Dest:   ".github/workflows/qegg-release.yml",
		Perm:   0o644,
		Reason: "release workflow",
		Enabled: func(opts Options) bool {
			return opts.WithWorkflows && opts.Workflow.Mode != config.WorkflowExternal
		},
	},
	{
		ID:      "workflow-pr",
		Source:  "github/workflows/qegg-pr.yml.tmpl",
		Dest:    ".github/workflows/qegg-pr.yml",
		Perm:    0o644,
		Reason:  "pull request workflow",
		Enabled: func(opts Options) bool { return opts.WithWorkflows },
	},
	{
		ID:      "workflow-status",
		Source:  "github/workflows/qegg-status.yml.tmpl",
		Dest:    ".github/workflows/qegg-status.yml",
		Perm:    0o644,
		Reason:  "status workflow",
		Enabled: func(opts Options) bool { return opts.WithWorkflows && opts.WithStatus },
	},
}

// ManagedPaths lists the repository-relative files qegg writes itself. The
// pre-commit rule ignores them, so scaffolding and generated updates never
// need a change record.
func ManagedPaths(changesDir string) []string {
	paths := []string{config.FileName}
	if dir := filepath.ToSlash(filepath.Clean(changesDir)); dir != "." && dir != "" {
		paths = append(paths, dir+"/README.md")
	}
	for _, item := range artifacts {
		paths = append(paths, filepath.ToSlash(item.Dest))
	}
	paths = append(paths, gitignoreName)
	return paths
}

// view is the data every scaffold template renders with. Options is embedded
// so templates can refer to fields such as BaseBranch and QeggVersion.
type view struct {
	Options
	EcosystemSetups []string
	EcosystemEnvs   []string
	BuildJobs       []buildJobView
	ReleaseEnv      string
	ReleasePaths    []string
	HookNote        string
	// ActorGuard skips runs started by the release commit itself.
	ActorGuard string
	// BuildCondition gates the platform builds on the run's release phase.
	BuildCondition string
	// ReleaseJobs are the jobs that run qegg release. PR mode splits the work
	// into a version job (no builds) and a publish job (after the builds).
	ReleaseJobs []releaseJobView
}

// buildJobView is one platform build rendered into the release workflow.
type buildJobView struct {
	ID        string
	Runner    string
	Timeout   int
	Steps     []string
	YAML      string
	Env       string
	Artifacts []string
}

// releaseJobView is one job of the generated workflow that runs qegg release.
type releaseJobView struct {
	Name      string
	Needs     []string
	Condition string
	// Downloads is set when the job attaches the platform build artifacts.
	Downloads bool
}

// Job conditions of the generated release workflow. They read the phase the
// plan job resolved, so platform builds only start when the run publishes.
const (
	actorGuard       = "github.actor != 'github-actions[bot]'"
	conditionVersion = "needs.plan.outputs.phase == 'version'"
	conditionPublish = "needs.plan.outputs.phase == 'publish'"
	conditionAny     = "needs.plan.outputs.phase != 'none'"
)

// newView collects the template data for one plan.
func newView(opts Options) view {
	data := view{
		Options:        opts,
		ReleasePaths:   releasePaths(opts),
		HookNote:       i18n.T("scaffold.hook_note"),
		ActorGuard:     actorGuard,
		BuildCondition: actorGuard,
		ReleaseJobs:    []releaseJobView{{Name: "release", Condition: actorGuard}},
	}
	data.ReleaseEnv = envBlock(opts.Workflow.Env)
	for _, job := range opts.Build {
		data.BuildJobs = append(data.BuildJobs, buildJobView{
			ID:        job.Name,
			Runner:    job.Runner,
			Timeout:   job.TimeoutMinutes,
			Steps:     job.Steps,
			YAML:      job.YAML,
			Env:       envBlock(job.Env),
			Artifacts: job.Artifacts,
		})
	}
	if len(opts.Build) > 0 {
		needs := make([]string, 0, len(opts.Build)+1)
		needs = append(needs, "plan")
		for _, job := range opts.Build {
			needs = append(needs, "build-"+job.Name)
		}
		if opts.Mode == config.ModePR {
			data.BuildCondition = conditionPublish
			data.ReleaseJobs = []releaseJobView{
				{Name: "version", Needs: []string{"plan"}, Condition: conditionVersion},
				{Name: "publish", Needs: needs, Condition: conditionPublish, Downloads: true},
			}
		} else {
			data.BuildCondition = conditionAny
			data.ReleaseJobs = []releaseJobView{
				{Name: "release", Needs: needs, Condition: conditionAny, Downloads: true},
			}
		}
	}
	for _, id := range workflowEcosystems(opts) {
		spec, ok := registry.SpecFor(id)
		if !ok {
			continue
		}
		if strings.TrimSpace(spec.WorkflowSetup) != "" {
			data.EcosystemSetups = append(data.EcosystemSetups, spec.WorkflowSetup)
		}
		if strings.TrimSpace(spec.WorkflowEnv) != "" {
			data.EcosystemEnvs = append(data.EcosystemEnvs, spec.WorkflowEnv)
		}
	}
	return data
}

// workflowEcosystems lists the ecosystems the release workflow must set up and
// authenticate. Explicit publish targets win; otherwise every configured
// package contributes its resolver; the detected ecosystem is the fallback so
// a project without packages still gets a usable workflow.
func workflowEcosystems(opts Options) []string {
	var ids []string
	seen := map[string]bool{}
	add := func(target string) {
		id := ecosystemID(target)
		if id == "" || seen[id] {
			return
		}
		if _, ok := registry.SpecFor(id); !ok {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	for _, target := range opts.Registries {
		add(target)
	}
	if len(ids) == 0 {
		resolvers := make([]string, 0, len(opts.Packages))
		for _, pkg := range opts.Packages {
			resolvers = append(resolvers, pkg.Resolver)
		}
		sort.Strings(resolvers)
		for _, resolver := range resolvers {
			add(resolver)
		}
	}
	if len(ids) == 0 {
		add(opts.Ecosystem)
	}
	return ids
}

// envBlock renders a job environment mapping, sorted by variable name.
func envBlock(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key + ": " + yamlQuote(env[key]) + "\n")
	}
	return b.String()
}

// yamlQuote renders s as a double quoted YAML scalar.
func yamlQuote(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)
	return "\"" + replacer.Replace(s) + "\""
}

// ecosystemID maps a publish target name to its ecosystem identifier.
func ecosystemID(name string) string {
	if _, ok := registry.SpecFor(name); ok {
		return name
	}
	id, _ := registry.EcosystemForTarget(name)
	return id
}
