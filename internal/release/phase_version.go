package release

import (
	"context"
	"errors"
	"github.com/SuperQuail/QuailEgg/internal/changelog"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/verify"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// releasePRTitle stays English because a squash merge copies it into history.
const releasePRTitle = "chore(release): bump versions"

func releaseBranchName(configured string) string {
	if configured == "" {
		return "release"
	}
	return configured
}

// releaseRefspec force-updates the remote release branch, because every run
// rebuilds it from the current default branch instead of appending to it.
func releaseRefspec(branch string) string {
	return "+HEAD:refs/heads/" + branch
}

func (e *executor) releaseBranch() string {
	return releaseBranchName(e.d.Cfg.Release.Branch)
}

func (e *executor) runVersion(ctx context.Context) error {
	if err := e.step(ctx, "preflight", e.preflightVersion); err != nil {
		return err
	}
	if err := e.step(ctx, "verify", e.verify); err != nil {
		return err
	}
	if e.d.Cfg.Release.Mode == config.ModePR {
		if err := e.step(ctx, "branch", e.prepareReleaseBranch); err != nil {
			return err
		}
	}
	if err := e.step(ctx, "bump", e.bump); err != nil {
		return err
	}
	if err := e.step(ctx, "changelog", e.writeChangelog); err != nil {
		return err
	}
	if err := e.step(ctx, "commit", e.commit); err != nil {
		return err
	}
	if e.d.Cfg.Release.Mode == config.ModePR {
		return e.openPullRequest(ctx)
	}
	return e.runPublish(ctx)
}

// prepareReleaseBranch moves the working tree onto a branch that the next run
// rebuilds from scratch. The version commit therefore never lands on main.
func (e *executor) prepareReleaseBranch(ctx context.Context) error {
	branch := e.releaseBranch()
	if branch == e.d.Cfg.Project.DefaultBranch {
		return errors.New(i18n.Tf("release.err.release_branch_is_base", branch))
	}
	return e.d.Git.CheckoutBranch(ctx, branch)
}

func (e *executor) preflightVersion(ctx context.Context) error {
	if !e.d.Options.AllowDirty && !isCI() {
		clean, err := e.d.Git.IsCleanExcept(ctx, state.RuntimePaths())
		if err != nil {
			return err
		}
		if !clean {
			return errors.New(i18n.T("release.err.dirty"))
		}
	}
	if isCI() || e.d.Cfg.Release.Mode == config.ModePR {
		branch, err := e.d.Git.Branch(ctx)
		if err != nil {
			return err
		}
		if branch != e.d.Cfg.Project.DefaultBranch {
			return errors.New(i18n.Tf("release.err.branch", e.d.Cfg.Project.DefaultBranch, branch))
		}
	}
	return nil
}

func (e *executor) verify(ctx context.Context) error {
	if e.d.Options.SkipVerify {
		e.skip("verify", i18n.T("release.skip.skip_verify"))
		return nil
	}
	timeout, err := time.ParseDuration(e.d.Cfg.Verify.Timeout)
	if err != nil {
		timeout = 15 * time.Minute
	}
	result, err := verify.RunVerification(ctx, e.d.Run, e.d.Root, e.d.Cfg.Verify.Commands, timeout, e.d.Cfg.Verify.FailFast, os.Stdout)
	if len(result.Commands) == 0 {
		e.d.UI.Warn("%s", i18n.T("release.warn.no_verify"))
	}
	return err
}

func (e *executor) bump(ctx context.Context) error {
	inspected, err := e.inspectPlanPackages()
	if err != nil {
		return err
	}
	if e.plan.VersionScope != config.VersionScopeShared {
		if inherited := inheritedWorkspaceVersions(inspected); len(inherited) > 1 {
			return errors.New(i18n.Tf("release.err.version_inherited", inherited[0], inherited[1]))
		}
	}
	next := map[string]string{}
	for _, release := range e.plan.Releases {
		for _, name := range release.Packages {
			next[name] = release.Version
		}
	}
	for _, pkg := range e.plan.Packages {
		target, ok := findInspected(inspected, pkg.Name)
		if !ok {
			return errors.New(i18n.Tf("release.err.package_missing", pkg.Name))
		}
		current, err := registry.CurrentVersion(ctx, e.d.Run, e.d.Root, target, e.d.Cfg.Git.TagPrefix)
		if err != nil {
			return err
		}
		if current != pkg.Current {
			return errors.New(i18n.Tf("release.err.version_changed", pkg.Name, pkg.Current, current))
		}
		value := next[pkg.Name]
		if value == "" {
			continue
		}
		if err := registry.SetVersion(e.d.Root, target, value); err != nil {
			return err
		}
		e.d.UI.Info("%s: %s -> %s", pkg.Name, pkg.Current, value)
	}
	if err := e.syncWorkspace(ctx, inspected); err != nil {
		return err
	}
	return e.writeVersionFiles()
}

// inspectPlanPackages reads every planned package from its configured path.
func (e *executor) inspectPlanPackages() ([]ecosystem.Package, error) {
	inputs := make([]ecosystem.Package, 0, len(e.plan.Packages))
	for _, pkg := range e.plan.Packages {
		inputs = append(inputs, ecosystem.Package{Name: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver})
	}
	return registry.InspectAll(e.d.Root, inputs)
}

// findInspected resolves a planned package by name.
func findInspected(pkgs []ecosystem.Package, name string) (ecosystem.Package, bool) {
	for _, pkg := range pkgs {
		if pkg.Name == name {
			return pkg, true
		}
	}
	return ecosystem.Package{}, false
}

// syncWorkspace keeps sibling references (dependency pins, lock files) in sync
// after the manifests were bumped. Every package is passed with the version to
// write; ecosystems without a VersionSyncer have nothing to sync.
func (e *executor) syncWorkspace(ctx context.Context, inspected []ecosystem.Package) error {
	next := map[string]string{}
	for _, release := range e.plan.Releases {
		for _, name := range release.Packages {
			next[name] = release.Version
		}
	}
	groups := map[string][]ecosystem.Package{}
	for _, planned := range e.plan.Packages {
		target, ok := findInspected(inspected, planned.Name)
		if !ok {
			continue
		}
		if version, ok := next[planned.Name]; ok {
			target.Version = version
		}
		groups[planned.Resolver] = append(groups[planned.Resolver], target)
	}
	resolvers := make([]string, 0, len(groups))
	for resolver := range groups {
		resolvers = append(resolvers, resolver)
	}
	sort.Strings(resolvers)
	var synced []string
	for _, resolver := range resolvers {
		syncer, ok := registry.VersionSyncerFor(resolver)
		if !ok {
			continue
		}
		paths, err := syncer.SyncVersions(ctx, e.d.Run, e.d.Root, groups[resolver])
		if err != nil {
			return err
		}
		synced = append(synced, paths...)
	}
	e.syncPaths = append(e.syncPaths, synced...)
	return nil
}

func (e *executor) writeChangelog(ctx context.Context) error {
	if e.plan.Channel != "" {
		e.skip("changelog", i18n.T("release.skip.prerelease_changelog"))
		return nil
	}
	sections := ChangelogSections(e.plan, e.d.Now())
	path := filepath.Join(e.d.Root, "CHANGELOG.md")
	existing, err := fsx.ReadOrEmpty(path)
	if err != nil {
		return err
	}
	updated, err := changelog.UpdateAll(existing, sections)
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, updated, 0o644)
}

func (e *executor) commit(ctx context.Context) error {
	if err := state.SaveMarker(e.d.Root, e.plan.markerFile(e.d)); err != nil {
		return err
	}
	prerelease := e.plan.Channel != ""
	paths := []string{state.MarkerPath(e.d.Root)}
	if prerelease {
		lock := &state.PreRelease{Channel: e.plan.Channel, Version: e.plan.Version.Next}
		if err := state.SavePreRelease(e.d.Root, lock); err != nil {
			return err
		}
		paths = append(paths, state.PreReleasePath(e.d.Root))
	} else {
		paths = append(paths, filepath.Join(e.d.Root, "CHANGELOG.md"))
	}
	for _, pkg := range e.plan.Packages {
		if pkg.Manifest == "" {
			continue
		}
		paths = append(paths, fsx.Join(e.d.Root, pkg.Manifest))
	}
	for _, rel := range e.syncPaths {
		ignored, err := e.d.Git.Ignored(ctx, rel)
		if err != nil {
			return err
		}
		if ignored {
			continue
		}
		paths = append(paths, fsx.Join(e.d.Root, rel))
	}
	if err := e.d.Git.Add(ctx, paths...); err != nil {
		return err
	}
	if !prerelease {
		var records []string
		for _, change := range e.plan.Changes {
			records = append(records, change.File)
		}
		if err := e.d.Git.Remove(ctx, records...); err != nil {
			return err
		}
	}
	message := releaseCommitMessage(e.d.Cfg, e.plan)
	sha, err := e.d.Git.CommitNoVerify(ctx, message)
	if err != nil {
		return err
	}
	e.d.UI.Info("%s", i18n.Tf("release.msg.committed", message, shortSHA(sha)))
	return nil
}

// openPullRequest pushes the rebuilt branch and refreshes the single release
// pull request. The push is forced: the branch is regenerated from main on
// every run, so the previous tip is intentionally discarded.
func (e *executor) openPullRequest(ctx context.Context) error {
	branch := e.releaseBranch()
	if e.d.Options.NoPush {
		e.skip("push", i18n.T("release.skip.no_push"))
		return nil
	}
	if err := e.d.Git.Push(ctx, e.d.Cfg.Git.Remote, releaseRefspec(branch)); err != nil {
		return err
	}
	body := e.plan.pullRequestBody(e.d)
	if err := github.EnsurePR(ctx, e.d.Run, e.plan.Repo, branch, e.d.Cfg.Project.DefaultBranch, releasePRTitle, body); err != nil {
		return err
	}
	e.d.UI.Success("%s", i18n.Tf("release.msg.pr", releasePRTitle))
	return nil
}
