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
	if err := e.d.Git.Push(ctx, e.d.Cfg.Git.Remote, releaseRefspec(branch)); err != nil {
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
		if err := registry.Build(ctx, e.d.Run, e.d.Root, target); err != nil {
			return err
		}
		if err := registry.Publish(ctx, e.d.Run, e.d.Root, target, pkg.Next); err != nil {
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

func (e *executor) tag(ctx context.Context) error {
	for _, release := range e.plan.Releases {
		exists, err := e.d.Git.TagExists(ctx, release.Tag)
		if err != nil {
			return err
		}
		if exists {
			head, err := e.d.Git.HeadSHA(ctx)
			if err != nil {
				return err
			}
			points, err := e.d.Git.TagPointsAt(ctx, release.Tag, head)
			if err != nil {
				return err
			}
			if !points {
				return errors.New(i18n.Tf("release.err.tag_mismatch", release.Tag))
			}
			e.skip("tag", i18n.T("release.skip.tag_exists"))
			continue
		}
		if err := e.d.Git.CreateTag(ctx, release.Tag, "Release "+release.Tag); err != nil {
			return err
		}
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
	prerelease := e.plan.Channel != ""
	for _, release := range e.plan.Releases {
		notes, ok := e.releaseNotes(release)
		if !ok {
			notes = "Release " + release.Tag
		}
		err := github.CreateRelease(ctx, e.d.Run, e.plan.Repo, release.Tag, release.Tag, notes, prerelease || release.Prerelease != "")
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
	if err := github.UploadReleaseAssets(ctx, e.d.Run, tag, e.run.Assets); err != nil {
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

func (e *executor) releaseNotes(release ReleasePlan) (string, bool) {
	if e.plan.Channel != "" || release.Prerelease != "" {
		for _, section := range ChangelogSections(e.plan, e.d.Now()) {
			if section.Package == release.Name && section.Version == release.Version {
				return changelog.RenderBody(section), true
			}
		}
		return "", false
	}
	path := filepath.Join(e.d.Root, "CHANGELOG.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	key := changelog.Section{Version: release.Version, Package: release.Name}.Key()
	return changelog.Extract(data, key)
}
