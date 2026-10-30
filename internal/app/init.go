package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/scaffold"
	"path/filepath"
)

// InitOptions carries the flags of qegg init.
type InitOptions struct {
	Mode           string
	Branch         string
	TagPrefix      string
	Remote         string
	Registries     []string
	ChangesDir     string
	Scope          []string
	VerifyCommands []string
	NoVerify       bool
	WithHook       bool
	WithWorkflows  bool
	WithStatus     bool
	AddPackages    bool
	Force          bool
	DryRun         bool
	QeggVersion    string
	Environment    string
}

// Init plans (and optionally applies) the repository scaffolding.
func Init(ctx context.Context, cwd string, printer *ui.Printer, opts InitOptions) (*scaffold.Plan, error) {
	root, err := fsx.RepoRoot(cwd)
	if err != nil {
		return nil, errors.New(i18n.T("error.not_git_repo"))
	}
	detected, err := registry.DetectAll(root)
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(root, config.FileName)
	existingContent, err := fsx.ReadOrEmpty(configPath)
	if err != nil {
		return nil, err
	}
	var existing *config.Config
	if len(existingContent) > 0 {
		existing, err = config.LoadFile(configPath)
		if err != nil {
			return nil, err
		}
	}

	packages := map[string]config.Package{}
	if existing != nil {
		for name, pkg := range existing.Packages {
			packages[name] = pkg
		}
	}
	if opts.AddPackages {
		for _, pkg := range detected {
			if _, ok := packages[pkg.Name]; ok {
				continue
			}
			publish := pkg.Publish
			packages[pkg.Name] = config.Package{
				Path:     pkg.Path,
				Resolver: pkg.Resolver,
				Publish:  &publish,
			}
		}
	}
	if existing == nil && len(packages) == 0 {
		return nil, errors.New(i18n.Tf("error.no_packages", registry.ManifestListText()))
	}

	versionScope := config.VersionScopePackage
	if existing != nil && existing.Release.VersionScope != "" {
		versionScope = existing.Release.VersionScope
	} else if len(packages) > 1 && uniformVersions(detected) {
		versionScope = config.VersionScopeShared
	}
	environment := opts.Environment
	if environment == "" && existing != nil {
		environment = existing.Release.Environment
	}

	baseBranch := opts.Branch
	if baseBranch == "" && existing != nil {
		baseBranch = existing.Project.DefaultBranch
	}
	if baseBranch == "" {
		baseBranch = detectDefaultBranch(ctx, root)
	}
	mode := opts.Mode
	if mode == "" && existing != nil {
		mode = existing.Release.Mode
	}
	if mode == "" {
		mode = config.ModePR
	}
	tagPrefix := opts.TagPrefix
	if tagPrefix == "" && existing != nil {
		tagPrefix = existing.Git.TagPrefix
	}
	if tagPrefix == "" {
		tagPrefix = "v"
	}
	remote := opts.Remote
	if remote == "" && existing != nil {
		remote = existing.Git.Remote
	}
	if remote == "" {
		remote = "origin"
	}
	repoID, repoWarn, err := detectRemoteRepo(ctx, root, remote, existing)
	if err != nil {
		return nil, err
	}
	changesDir := opts.ChangesDir
	if changesDir == "" && existing != nil {
		changesDir = existing.Changes.Dir
	}
	if changesDir == "" {
		changesDir = ".changes"
	}
	scope := opts.Scope
	if scope == nil && existing != nil {
		scope = existing.Changes.Scope
	}
	if scope == nil {
		scope = defaultScope(packages)
	}
	registries := opts.Registries
	if registries == nil && existing != nil {
		registries = existing.Publish.Registries
	}
	if len(registries) == 0 {
		registries = inferRegistries(packages)
	}
	ecosystemID := ""
	if len(registries) > 0 {
		ecosystemID = registries[0]
	} else if len(detected) > 0 {
		ecosystemID = detected[0].Resolver
	}

	verifyCommands := opts.VerifyCommands
	if opts.NoVerify {
		verifyCommands = []string{}
	} else if verifyCommands == nil && existing != nil {
		verifyCommands = existing.Verify.Commands
	}
	if verifyCommands == nil {
		verifyCommands = defaultVerifyCommands(root, ecosystemID)
	}

	releaseBranch := config.Default().Release.Branch
	if existing != nil && existing.Release.Branch != "" {
		releaseBranch = existing.Release.Branch
	}

	version := opts.QeggVersion
	if version == "" {
		version = "latest"
	}

	plan, err := scaffold.Build(scaffold.Options{
		Root:            root,
		BaseBranch:      baseBranch,
		ReleaseBranch:   releaseBranch,
		Mode:            mode,
		TagPrefix:       tagPrefix,
		Remote:          remote,
		Repo:            repoID,
		Registries:      registries,
		ChangesDir:      changesDir,
		Scope:           scope,
		VerifyCommands:  verifyCommands,
		Ecosystem:       ecosystemID,
		QeggVersion:     version,
		VersionScope:    versionScope,
		Environment:     environment,
		WithHook:        opts.WithHook,
		WithWorkflows:   opts.WithWorkflows,
		WithStatus:      opts.WithStatus,
		AddPackages:     opts.AddPackages,
		Force:           opts.Force,
		Packages:        packages,
		Existing:        existing,
		ExistingContent: existingContent,
	})
	if err != nil {
		return nil, err
	}
	if repoWarn != "" {
		plan.Warnings = append(plan.Warnings, repoWarn)
	}
	if !opts.DryRun {
		if _, err := scaffold.Apply(plan); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// detectRemoteRepo resolves the configured git remote into the canonical
// host/owner/name identity of the project repository. A missing remote is an
// error because the identity is what lets a fork clone be recognised.
func detectRemoteRepo(ctx context.Context, root, remote string, existing *config.Config) (string, string, error) {
	run := execx.OSRunner{}
	res, err := run.Run(ctx, root, "git", "remote", "get-url", remote)
	if err != nil {
		return "", "", errors.New(i18n.Tf("init.err.no_remote", remote, remote))
	}
	repo, ok := github.ParseRemote(res.Stdout)
	if !ok {
		return "", i18n.Tf("init.warn.repo_unknown", remote), nil
	}
	id := repo.ID()
	if existing != nil && strings.TrimSpace(existing.Project.Repo) != "" && !strings.EqualFold(existing.Project.Repo, id) {
		return id, i18n.Tf("init.warn.repo_differs", remote, id, existing.Project.Repo), nil
	}
	return id, "", nil
}

// uniformVersions reports whether every detected package has the same
// manifest version under one resolver, which is what shared versioning needs.
func uniformVersions(detected []ecosystem.Package) bool {
	if len(detected) < 2 {
		return false
	}
	first := detected[0]
	if first.Version == "" || first.Resolver == config.ResolverGo {
		return false
	}
	for _, pkg := range detected[1:] {
		if pkg.Version != first.Version || pkg.Resolver != first.Resolver {
			return false
		}
	}
	return true
}

// inferRegistries maps the configured packages to publish targets.
func inferRegistries(packages map[string]config.Package) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, pkg := range packages {
		target := ""
		switch pkg.Resolver {
		case config.ResolverNPM:
			target = "npm"
		case config.ResolverPython:
			target = "pypi"
		case config.ResolverRust:
			target = "crates"
		case config.ResolverGo:
			target = "go"
		}
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}
