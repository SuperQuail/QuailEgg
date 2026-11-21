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
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"os"
	"path/filepath"
)

func (e *executor) runPublish(ctx context.Context) error {
	if err := e.step(ctx, "preflight", e.preflightPublish); err != nil {
		return err
	}
	// The release snapshot is pinned before anything is built, uploaded or
	// tagged: a branch that moved on must not reach the release.
	if err := e.step(ctx, "source", e.resolveSource); err != nil {
		return err
	}
	if err := e.step(ctx, "assets", e.buildAssets); err != nil {
		return err
	}
	if err := e.step(ctx, "publish", e.publishPackages); err != nil {
		return err
	}
	if err := e.step(ctx, "tag", e.tag); err != nil {
		return err
	}
	if err := e.step(ctx, "push", e.push); err != nil {
		return err
	}
	if err := e.step(ctx, "github-release", e.githubRelease); err != nil {
		return err
	}
	if e.d.Cfg.Release.Mode == config.ModePR {
		return e.step(ctx, "sync", e.syncReleaseBranch)
	}
	return nil
}

// syncReleaseBranch flattens the release branch onto the published commit once
// the release is out, so an idle branch never lags behind main. A branch that
// GitHub already deleted stays deleted and is recreated by the next release.
func (e *executor) syncReleaseBranch(ctx context.Context) error {
	if e.d.Options.NoPush {
		e.skip("sync", i18n.T("release.skip.no_push"))
		return nil
	}
	branch := e.releaseBranch()
	exists, err := e.d.Git.RemoteBranchExists(ctx, e.d.Cfg.Git.Remote, branch)
	if err != nil {
		e.d.UI.Warn("%s", i18n.Tf("release.warn.sync_failed", err))
		return nil
	}
	if !exists {
		return nil
	}
	source := e.identity.SourceCommit
	if source == "" {
		// A run that never pinned a snapshot must not flatten the branch onto
		// whatever happens to be checked out.
		e.d.UI.Warn("%s", i18n.T("release.err.source_unset"))
		return nil
	}
	if err := e.d.Git.Push(ctx, e.d.Cfg.Git.Remote, releaseRefspecAt(source, branch)); err != nil {
		e.d.UI.Warn("%s", i18n.Tf("release.warn.sync_failed", err))
	}
	return nil
}

func (e *executor) preflightPublish(ctx context.Context) error {
	rel, err := state.LoadMarker(e.d.Root)
	if err != nil {
		return err
	}
	if rel == nil {
		return errors.New(i18n.T("release.err.no_marker"))
	}
	if rel.Version != "" && e.plan.Version.Next != "" && rel.Version != e.plan.Version.Next {
		return errors.New(i18n.Tf("release.err.marker_mismatch", rel.Version, e.plan.Version.Next))
	}
	planned := map[string]bool{}
	for _, release := range e.plan.Releases {
		planned[release.Tag] = true
	}
	for _, release := range rel.Releases {
		if len(e.plan.Releases) > 0 && !planned[release.Tag] {
			return errors.New(i18n.Tf("release.err.marker_mismatch", release.Version, e.plan.Version.Next))
		}
	}
	return nil
}

func (e *executor) publishPackages(ctx context.Context) error {
	if e.d.Options.NoPublish {
		e.skip("publish", i18n.T("release.skip.no_publish"))
		return nil
	}
	for _, pkg := range e.plan.Packages {
		if !pkg.Publish {
			e.setPackageState(pkg.Name, "skipped", "publish=false")
			continue
		}
		manifestName := pkg.Name
		if cfgPkg, ok := e.d.Cfg.Packages[pkg.Name]; ok {
			manifestName = config.ManifestNameOf(pkg.Name, cfgPkg)
		}
		target := ecosystem.Package{Name: manifestName, Identity: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver}
		status, err := registry.Precheck(ctx, e.d.HTTP, target, pkg.Next)
		if err != nil {
			return err
		}
		if status == ecosystem.Exists {
			e.setPackageState(pkg.Name, "skipped-exists", "already published")
			e.d.UI.Info("%s", i18n.Tf("release.msg.registry_exists", pkg.Name, pkg.Next))
			continue
		}
		root, err := e.sourceRoot(ctx)
		if err != nil {
			return err
		}
		if err := registry.Build(ctx, e.d.Run, root, target); err != nil {
			return err
		}
		if err := registry.Publish(ctx, e.d.Run, root, target, pkg.Next); err != nil {
			return err
		}
		e.setPackageState(pkg.Name, state.StatusDone, "")
		e.d.UI.Success("%s", i18n.Tf("release.msg.published", pkg.Name, pkg.Next))
	}
	return nil
}

func (e *executor) setPackageState(name, status, reason string) {
	if e.run.Publish == nil {
		e.run.Publish = map[string]state.PackageState{}
	}
	e.run.Publish[name] = state.PackageState{Status: status, Reason: reason}
	state.SaveRun(e.d.Root, e.run)
}

// tag creates every release tag at the pinned snapshot. Checking an existing
// tag against the snapshot, and not against HEAD, keeps a resumed release from
// accepting a tag that names whichever commit happens to be checked out.
func (e *executor) tag(ctx context.Context) error {
	source := e.identity.SourceCommit
	if source == "" {
		return errors.New(i18n.T("release.err.source_unset"))
	}
	for _, release := range e.plan.Releases {
		exists, err := e.d.Git.TagExists(ctx, release.Tag)
		if err != nil {
			return err
		}
		if exists {
			points, err := e.d.Git.TagPointsAt(ctx, release.Tag, source)
			if err != nil {
				return err
			}
			if !points {
				return errors.New(i18n.Tf("release.err.tag_mismatch", release.Tag))
			}
			e.skip("tag", i18n.T("release.skip.tag_exists"))
			continue
		}
		if err := e.d.Git.CreateTagAt(ctx, release.Tag, "Release "+release.Tag, source); err != nil {
			return err
		}
	}
	return nil
}

// checkRemoteTags stops on a remote tag that already names another commit,
// before anything is uploaded or built. Pushing over it would either fail late
// or hide that the release snapshot is not what the remote published.
func (e *executor) checkRemoteTags(ctx context.Context, source string) error {
	if source == "" || !e.consultsRemoteTags() {
		return nil
	}
	for _, release := range e.plan.Releases {
		if err := e.checkRemoteTag(ctx, release.Tag, source); err != nil {
			return err
		}
	}
	return nil
}

// consultsRemoteTags reports whether the remote can disagree about a tag this
// run cares about. A run that neither pushes tags nor creates a GitHub Release
// touches no remote tag at all, so it does not need the lookup.
func (e *executor) consultsRemoteTags() bool {
	if e.d.Cfg.Git.Remote == "" {
		return false
	}
	pushes := !e.d.Options.NoPush
	releases := !e.d.Options.NoGitHubRelease && e.d.Cfg.Release.GitHubRelease
	return pushes || releases
}

// checkRemoteTag stops on a remote tag that already names another commit.
func (e *executor) checkRemoteTag(ctx context.Context, tag, source string) error {
	if e.d.Cfg.Git.Remote == "" {
		return nil
	}
	remote, found, err := e.d.Git.RemoteTagCommit(ctx, e.d.Cfg.Git.Remote, tag)
	if err != nil {
		return err
	}
	if found && !sameSourceCommit(remote, source) {
		return errors.New(i18n.Tf("release.err.remote_tag_mismatch", tag, shortSHA(remote), shortSHA(source)))
	}
	return nil
}

func (e *executor) push(ctx context.Context) error {
	if e.d.Options.NoPush {
		e.skip("push", i18n.T("release.skip.no_push"))
		return nil
	}
	branch, err := e.d.Git.Branch(ctx)
	if err != nil {
		return err
	}
	refs := []string{"HEAD:refs/heads/" + branch}
	if e.d.Cfg.Release.Mode == config.ModePR {
		refs = nil
	}
	for _, release := range e.plan.Releases {
		refs = append(refs, "refs/tags/"+release.Tag)
	}
	return e.d.Git.Push(ctx, e.d.Cfg.Git.Remote, refs...)
}

func (e *executor) githubRelease(ctx context.Context) error {
	if !e.d.Cfg.Release.GitHubRelease {
		e.skip("github-release", i18n.T("release.skip.github_disabled"))
		e.warnAssetsNotUploaded()
		return nil
	}
	if e.d.Options.NoGitHubRelease {
		e.skip("github-release", i18n.T("release.skip.no_release"))
		e.warnAssetsNotUploaded()
		return nil
	}
	ops := e.d.releaseOps()
	for _, release := range e.plan.Releases {
		title, notes, prerelease := e.releasePayload(release)
		err := ops.CreateRelease(ctx, e.plan.Repo, release.Tag, title, notes, prerelease)
		if errors.Is(err, github.ErrExists) {
			e.d.UI.Info("%s", i18n.Tf("release.msg.release_exists", release.Tag))
		} else if err != nil {
			return err
		} else {
			e.d.UI.Success("%s", i18n.Tf("release.msg.release_created", release.Tag))
		}
	}
	if len(e.run.Assets) == 0 || len(e.plan.Releases) != 1 {
		return nil
	}
	tag := e.plan.Releases[0].Tag
	if err := ops.UploadReleaseAssets(ctx, e.plan.Repo, tag, e.run.Assets, true); err != nil {
		return err
	}
	e.d.UI.Success("%s", i18n.Tf("release.msg.assets_uploaded", len(e.run.Assets), tag))
	return nil
}

// warnAssetsNotUploaded surfaces built assets that never reached a release.
func (e *executor) warnAssetsNotUploaded() {
	if len(e.run.Assets) > 0 {
		e.d.UI.Warn("%s", i18n.T("release.warn.assets_not_uploaded"))
	}
}

// releasePayload renders what a GitHub Release is created with: its title, the
// notes of the recorded release and the prerelease flag. A repair reuses this,
// so a recovered release keeps the notes and the flag of the original run.
func (e *executor) releasePayload(release ReleasePlan) (title, notes string, prerelease bool) {
	notes, ok := e.releaseNotes(release)
	if !ok {
		notes = "Release " + release.Tag
	}
	return release.Tag, notes, e.plan.Channel != "" || release.Prerelease != ""
}

func (e *executor) releaseNotes(release ReleasePlan) (string, bool) {
	if !e.d.Cfg.Release.ChangelogEnabled() {
		return "", false
	}
	if e.plan.Channel != "" || release.Prerelease != "" {
		for _, section := range ChangelogSections(e.plan, e.d.Now()) {
			if section.Package == release.Name && section.Version == release.Version {
				return changelog.RenderBody(section), true
			}
		}
		return "", false
	}
	data := e.changelog
	if data == nil {
		file, err := os.ReadFile(filepath.Join(e.d.Root, "CHANGELOG.md"))
		if err != nil {
			return "", false
		}
		data = file
	}
	key := changelog.Section{Version: release.Version, Package: release.Name}.Key()
	return changelog.Extract(data, key)
}
