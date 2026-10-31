package app

import (
	"context"
	"errors"
	"path"
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
	"github.com/SuperQuail/QuailEgg/internal/semver"
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
	// Packages limits the new packages init registers. Nil registers every
	// selectable candidate; a non-nil empty slice registers none.
	Packages []string
	// ScanDepth bounds the manifest discovery. Zero keeps only workspace
	// detection.
	ScanDepth int
	// Exclude lists repository-relative globs skipped during discovery.
	Exclude []string
	// Candidates carries a prior Scan result so init does not discover twice.
	Candidates *ScanResult
}

// Init plans (and optionally applies) the repository scaffolding.
func Init(ctx context.Context, cwd string, printer *ui.Printer, opts InitOptions) (*scaffold.Plan, error) {
	root, err := fsx.RepoRoot(cwd)
	if err != nil {
		return nil, errors.New(i18n.T("error.not_git_repo"))
	}
	existing, existingContent, err := loadExistingConfig(root)
	if err != nil {
		return nil, err
	}
	scan := opts.Candidates
	if scan == nil {
		scan, err = Scan(ctx, root, ScanOptions{Depth: opts.ScanDepth, Exclude: opts.Exclude, Remote: opts.Remote})
		if err != nil {
			return nil, err
		}
	}
	chosen, err := initSelection(scan, opts)
	if err != nil {
		return nil, err
	}
	packages := map[string]config.Package{}
	if existing != nil {
		for name, pkg := range existing.Packages {
			packages[name] = pkg
		}
	}
	if opts.AddPackages {
		for _, candidate := range chosen {
			if _, ok := packages[candidate.Key]; ok {
				continue
			}
			publish := candidate.pkg.Publish
			packages[candidate.Key] = config.Package{
				Name:     candidate.Name,
				Path:     candidate.pkg.Path,
				Resolver: candidate.pkg.Resolver,
				Publish:  &publish,
			}
		}
	}
	if existing == nil && len(packages) == 0 {
		if len(scan.Packages) > 0 {
			return nil, errors.New(i18n.T("error.no_packages_selected"))
		}
		return nil, errors.New(i18n.Tf("error.no_packages", registry.ManifestListText()))
	}
	detected := selectedEcosystemPackages(chosen, packages)
	resolvers := packageResolvers(packages)
	if unknown := registry.UnknownResolvers(resolvers); len(unknown) > 0 {
		return nil, errors.New(i18n.Tf("init.err.unknown_resolver", strings.Join(unknown, ", "), strings.Join(registry.EcosystemIDs(), ", ")))
	}

	versionScope := config.VersionScopePackage
	if existing != nil && existing.Release.VersionScope != "" {
		versionScope = existing.Release.VersionScope
	} else if len(packages) > 1 && uniformVersions(detected) {
		versionScope = config.VersionScopeShared
	}
	if versionScope == config.VersionScopeShared && len(packages) > 1 {
		for _, name := range sortedPackageNames(packages) {
			if registry.IsTagVersioned(packages[name].Resolver) {
				return nil, errors.New(i18n.Tf("init.err.shared_tag_versioned", name))
			}
		}
	}
	// A version file carries one version, so it is only registered when the
	// release has exactly one: a single package or a shared scope.
	var versionFiles []config.VersionFile
	if len(packages) <= 1 || versionScope == config.VersionScopeShared {
		versionFiles = suggestedVersionFiles(root, packages, existing)
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
	if unknown := registry.UnknownRegistries(registries); len(unknown) > 0 {
		return nil, errors.New(i18n.Tf("init.err.unknown_registry", strings.Join(unknown, ", "), strings.Join(registry.PublishTargets(), ", ")))
	}
	ecosystemID := ""
	if len(registries) > 0 {
		ecosystemID, _ = registry.EcosystemForTarget(registries[0])
	}
	if ecosystemID == "" && len(detected) > 0 {
		ecosystemID = detected[0].Resolver
	}

	verifyCommands := opts.VerifyCommands
	if opts.NoVerify {
		verifyCommands = []string{}
	} else if verifyCommands == nil && existing != nil {
		verifyCommands = existing.Verify.Commands
	}
	if verifyCommands == nil {
		verifyCommands = registry.DefaultVerifyCommands(root, resolverIDs(detected, packages))
	}

	releaseBranch := config.Default().Release.Branch
	if existing != nil && existing.Release.Branch != "" {
		releaseBranch = existing.Release.Branch
	}
	workflow := config.Default().Release.Workflow
	var buildJobs []config.BuildJob
	if existing != nil {
		workflow = existing.Release.Workflow
		buildJobs = existing.Release.Build
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
		VersionFiles:    versionFiles,
		Workflow:        workflow,
		Build:           buildJobs,
		Existing:        existing,
		ExistingContent: existingContent,
	})
	if err != nil {
		return nil, err
	}
	if repoWarn != "" {
		plan.Warnings = append(plan.Warnings, repoWarn)
	}
	if opts.AddPackages && opts.Packages == nil {
		skipped := 0
		for _, candidate := range scan.Packages {
			if !candidate.Registered && !candidate.Selectable {
				skipped++
			}
		}
		if skipped > 0 {
			plan.Warnings = append(plan.Warnings, i18n.Tf("init.warn.scan_skipped", skipped))
		}
	}
	if len(packages) > 1 && versionScope != config.VersionScopeShared {
		if skipped := unregisteredVersionFiles(root, packages, existing); len(skipped) > 0 {
			plan.Warnings = append(plan.Warnings, i18n.Tf("init.warn.version_file_skipped", strings.Join(skipped, ", ")))
		}
	}
	if len(packages) > 1 && versionScope != config.VersionScopeShared {
		if names := duplicateManifestNames(packages); len(names) > 0 {
			plan.Warnings = append(plan.Warnings, i18n.Tf("init.warn.shared_recommended", strings.Join(names, "; ")))
		}
	}
	if warning := inheritedSelectionWarning(scan, chosen); warning != "" {
		plan.Warnings = append(plan.Warnings, warning)
	}
	if !opts.DryRun {
		if _, err := scaffold.Apply(plan); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// loadExistingConfig reads .quailegg.toml and its raw content when present.
func loadExistingConfig(root string) (*config.Config, []byte, error) {
	configPath := filepath.Join(root, config.FileName)
	content, err := fsx.ReadOrEmpty(configPath)
	if err != nil {
		return nil, nil, err
	}
	if len(content) == 0 {
		return nil, nil, nil
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, content, nil
}

// initSelection resolves the packages init is allowed to register.
func initSelection(scan *ScanResult, opts InitOptions) ([]ScanPackage, error) {
	if !opts.AddPackages {
		return nil, nil
	}
	if opts.Packages == nil {
		var out []ScanPackage
		for _, pkg := range scan.Packages {
			if pkg.Selectable && !pkg.Registered {
				out = append(out, pkg)
			}
		}
		return out, nil
	}
	ids, err := SelectPackages(scan, opts.Packages)
	if err != nil {
		return nil, err
	}
	byID := map[string]ScanPackage{}
	for _, pkg := range scan.Packages {
		byID[pkg.ID] = pkg
	}
	out := make([]ScanPackage, 0, len(ids))
	for _, id := range ids {
		if pkg, ok := byID[id]; ok && !pkg.Registered {
			out = append(out, pkg)
		}
	}
	return out, nil
}

// selectedEcosystemPackages is the detection view limited to the chosen
// packages. When nothing new is chosen it falls back to the configured
// packages so resolvers and the ecosystem fallback stay meaningful.
func selectedEcosystemPackages(chosen []ScanPackage, packages map[string]config.Package) []ecosystem.Package {
	if len(chosen) > 0 {
		out := make([]ecosystem.Package, 0, len(chosen))
		for _, pkg := range chosen {
			out = append(out, pkg.pkg)
		}
		return out
	}
	names := sortedPackageNames(packages)
	out := make([]ecosystem.Package, 0, len(names))
	for _, name := range names {
		pkg := packages[name]
		out = append(out, ecosystem.Package{Name: name, Path: pkg.Path, Resolver: pkg.Resolver})
	}
	return out
}

// unregisteredVersionFiles lists version files qegg could write but does not
// register because the repository uses package scope with several packages.
func unregisteredVersionFiles(root string, packages map[string]config.Package, existing *config.Config) []string {
	known := map[string]bool{}
	if existing != nil {
		for _, file := range existing.Version.File {
			if rel, err := file.RepoPath(); err == nil {
				known[rel] = true
			}
		}
	}
	var out []string
	for _, candidate := range versionFileCandidates(root, packages) {
		if known[candidate.File.Path] {
			continue
		}
		out = append(out, candidate.File.Path)
	}
	return out
}

// duplicateManifestNames lists manifest names registered under several keys.
func duplicateManifestNames(packages map[string]config.Package) []string {
	groups := map[string][]string{}
	for key, pkg := range packages {
		name := config.ManifestNameOf(key, pkg)
		groups[name] = append(groups[name], key)
	}
	var out []string
	for _, keys := range groups {
		if len(keys) < 2 {
			continue
		}
		sort.Strings(keys)
		out = append(out, strings.Join(keys, ", "))
	}
	sort.Strings(out)
	return out
}

// inheritedSelectionWarning explains that a Cargo workspace version affects
// members that are not registered.
func inheritedSelectionWarning(scan *ScanResult, chosen []ScanPackage) string {
	selected := map[string]bool{}
	anyInherited := false
	for _, pkg := range chosen {
		selected[pkg.Name] = true
		if pkg.pkg.Inherited {
			anyInherited = true
		}
	}
	if !anyInherited {
		return ""
	}
	var unselected []string
	for _, pkg := range scan.Packages {
		if pkg.pkg.Inherited && !pkg.Registered && !selected[pkg.Name] {
			unselected = append(unselected, pkg.Name)
		}
	}
	if len(unselected) == 0 {
		return ""
	}
	sort.Strings(unselected)
	return i18n.Tf("init.warn.workspace_version_shared", strings.Join(unselected, ", "))
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

// uniformVersions reports whether every detected package has the same manifest
// version, which is what shared versioning needs. The packages may come from
// different ecosystems: a Tauri app keeps its Rust crates and its frontend on
// one version. Ecosystems whose version comes from tags never qualify.
func uniformVersions(detected []ecosystem.Package) bool {
	if len(detected) < 2 {
		return false
	}
	first := detected[0]
	if first.Version == "" || registry.IsTagVersioned(first.Resolver) {
		return false
	}
	for _, pkg := range detected[1:] {
		if pkg.Version != first.Version || registry.IsTagVersioned(pkg.Resolver) {
			return false
		}
	}
	return true
}

// versionFileCandidate is one extra version file and the package suggesting it.
type versionFileCandidate struct {
	File    config.VersionFile
	Package string
}

// versionFileCandidates reads the extra version files the ecosystems suggest.
// Only files that currently hold a literal semver are returned.
func versionFileCandidates(root string, packages map[string]config.Package) []versionFileCandidate {
	known := map[string]bool{}
	var out []versionFileCandidate
	for _, name := range sortedPackageNames(packages) {
		pkg := packages[name]
		candidates := registry.VersionFileCandidates(root, ecosystem.Package{Name: name, Path: pkg.Path, Resolver: pkg.Resolver})
		for _, candidate := range candidates {
			rel := path.Clean(path.Join(pkg.Path, filepath.ToSlash(candidate.Path)))
			if known[rel] {
				continue
			}
			file := config.VersionFile{Path: rel, Format: candidate.Format, Key: candidate.Key}
			value, err := file.Read(root)
			if err != nil {
				continue
			}
			if _, err := semver.Parse(value); err != nil {
				continue
			}
			known[rel] = true
			out = append(out, versionFileCandidate{File: file, Package: name})
		}
	}
	return out
}

// suggestedVersionFiles lists the extra version files the ecosystems know about.
// Paths the configuration already lists are skipped, and so are files that do
// not hold a literal version yet (Tauri may take it from Cargo instead).
func suggestedVersionFiles(root string, packages map[string]config.Package, existing *config.Config) []config.VersionFile {
	known := map[string]bool{}
	if existing != nil {
		for _, file := range existing.Version.File {
			if rel, err := file.RepoPath(); err == nil {
				known[rel] = true
			}
		}
	}
	var out []config.VersionFile
	for _, candidate := range versionFileCandidates(root, packages) {
		if known[candidate.File.Path] {
			continue
		}
		out = append(out, candidate.File)
	}
	return out
}

// inferRegistries maps the configured packages to publish targets.
func inferRegistries(packages map[string]config.Package) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, name := range sortedPackageNames(packages) {
		pkg := packages[name]
		if pkg.Publish != nil && !*pkg.Publish {
			continue
		}
		for _, target := range registry.PublishTargetsFor(pkg.Resolver) {
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			out = append(out, target)
		}
	}
	sort.Strings(out)
	return out
}

// packageResolvers lists the configured resolvers in package name order.
func packageResolvers(packages map[string]config.Package) []string {
	var out []string
	for _, name := range sortedPackageNames(packages) {
		out = append(out, packages[name].Resolver)
	}
	return out
}

// resolverIDs lists the resolvers detection found, falling back to the
// configured packages when the repository has no detected manifest.
func resolverIDs(detected []ecosystem.Package, packages map[string]config.Package) []string {
	out := make([]string, 0, len(detected))
	for _, pkg := range detected {
		out = append(out, pkg.Resolver)
	}
	if len(out) == 0 {
		return packageResolvers(packages)
	}
	return out
}

// sortedPackageNames returns the configured package names in deterministic order.
func sortedPackageNames(packages map[string]config.Package) []string {
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
