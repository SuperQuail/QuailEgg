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
		ID:      "workflow-release",
		Source:  "github/workflows/qegg-release.yml.tmpl",
		Dest:    ".github/workflows/qegg-release.yml",
		Perm:    0o644,
		Reason:  "release workflow",
		Enabled: func(opts Options) bool { return opts.WithWorkflows },
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
	ReleasePaths    []string
	HookNote        string
}

// newView collects the template data for one plan.
func newView(opts Options) view {
	data := view{
		Options:      opts,
		ReleasePaths: releasePaths(opts),
		HookNote:     i18n.T("scaffold.hook_note"),
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

// ecosystemID maps a publish target name to its ecosystem identifier.
func ecosystemID(name string) string {
	if _, ok := registry.SpecFor(name); ok {
		return name
	}
	id, _ := registry.EcosystemForTarget(name)
	return id
}
