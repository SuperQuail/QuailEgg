package app

import (
	"context"
	"errors"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
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
		Registries:      registries,
		ChangesDir:      changesDir,
		Scope:           scope,
		VerifyCommands:  verifyCommands,
		Ecosystem:       ecosystemID,
		QeggVersion:     version,
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
	if !opts.DryRun {
		if _, err := scaffold.Apply(plan); err != nil {
			return nil, err
		}
	}
	return plan, nil
}
