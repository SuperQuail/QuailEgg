package release

import (
	"context"
	"errors"
	"fmt"
	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/assets"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/semver"
	"regexp"
	"sort"
	"strconv"
)

// Build computes the plan for the current repository state.
func Build(ctx context.Context, d *Deps) (*Plan, error) {
	detected, err := registry.DetectAll(d.Root)
	if err != nil {
		return nil, err
	}
	registered := d.Cfg.Packages
	if len(registered) == 0 {
		if len(detected) != 1 {
			return nil, fmt.Errorf("no packages registered; run qegg init (detected %d packages)", len(detected))
		}
		name := detected[0].Name
		publishable := detected[0].Publish
		registered = map[string]config.Package{
			name: {Path: detected[0].Path, Resolver: detected[0].Resolver, Publish: &publishable},
		}
	}
	if len(registered) > 1 {
		return nil, ErrMultiPackage
	}

	var pkgName string
	var pkgCfg config.Package
	for name, candidate := range registered {
		pkgName, pkgCfg = name, candidate
	}
	target, ok := findDetected(detected, pkgName, pkgCfg.Path)
	if !ok {
		return nil, fmt.Errorf("package %q is registered but was not detected in the repository", pkgName)
	}
	currentVersion, err := registry.CurrentVersion(ctx, d.Run, d.Root, target, d.Cfg.Git.TagPrefix)
	if err != nil {
		return nil, err
	}
	target.Version = currentVersion
	tagVersioned := registry.IsTagVersioned(target.Resolver)

	known := make([]string, 0, len(registered))
	for name := range registered {
		known = append(known, name)
	}
	records, err := changes.LoadDir(changesDir(d))
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := record.Validate(known); err != nil {
			return nil, fmt.Errorf("%s: %w", record.Path, err)
		}
	}
	consumed := changes.Consumed(records)

	publishable := target.Publish
	if pkgCfg.Publish != nil {
		publishable = *pkgCfg.Publish
	}

	plan := &Plan{
		SchemaVersion: 1,
		Phase:         PhaseNone,
		HasChanges:    len(consumed) > 0,
		Project: ProjectInfo{
			Name:      pkgName,
			Ecosystem: target.Resolver,
			Manifest:  target.Manifest,
			Root:      d.Root,
		},
		Version: VersionInfo{Current: target.Version},
		Verify: VerifyInfo{
			Commands: append([]string(nil), d.Cfg.Verify.Commands...),
			Timeout:  d.Cfg.Verify.Timeout,
			Source:   "config",
		},
	}

	if target.Resolver == config.ResolverGo {
		assetPlan, assetWarnings, err := assets.Detect(d.Root, assets.Options{
			Enabled: d.Cfg.Release.Assets.Enabled,
			Name:    d.Cfg.Release.Assets.Name,
			Main:    d.Cfg.Release.Assets.Main,
			Targets: d.Cfg.Release.Assets.Targets,
			Ldflags: d.Cfg.Release.Assets.Ldflags,
		})
		if err != nil {
			return nil, err
		}
		plan.Assets = assetPlan
		plan.Warnings = append(plan.Warnings, assetWarnings...)
	}

	repo, _ := github.Detect(ctx, d.Run, d.Root, d.Cfg.Git.Remote)
	plan.Repo = repo

	relMarker, err := state.LoadMarker(d.Root)
	if err != nil {
		return nil, err
	}
	if relMarker != nil {
		plan.Marker = relMarker
		if relMarker.Repo.Valid() {
			plan.Repo = github.Repo{Host: relMarker.Repo.Host, Owner: relMarker.Repo.Owner, Name: relMarker.Repo.Name}
		}
	}

	tag := semver.Tag(d.Cfg.Git.TagPrefix, target.Version)
	tagExists, err := d.Git.TagExists(ctx, tag)
	if err != nil {
		return nil, err
	}
	markerTag := ""
	markerTagExists := false
	if relMarker != nil && relMarker.Version != "" {
		markerTag = semver.Tag(d.Cfg.Git.TagPrefix, relMarker.Version)
		markerTagExists, err = d.Git.TagExists(ctx, markerTag)
		if err != nil {
			return nil, err
		}
	}
	tags, err := d.Git.ListTags(ctx, d.Cfg.Git.TagPrefix)
	if err != nil {
		return nil, err
	}
	pre, err := state.LoadPreRelease(d.Root)
	if err != nil {
		return nil, err
	}
	previousTag := func() {
		if tagExists {
			plan.PreviousTag = tag
		}
	}

	switch {
	case relMarker != nil && !markerTagExists && markerApplies(relMarker.Version, target.Version, tagVersioned):
		plan.Phase = PhasePublish
		plan.Version = VersionInfo{Current: target.Version, Next: relMarker.Version}
		plan.Packages = markerPackages(relMarker.Packages)
		plan.Changes = markerChanges(relMarker.Changes)
		plan.Tag = markerTag
		plan.HasChanges = false
		plan.Channel = relMarker.Channel
		if plan.Channel == "" {
			if _, channel, _, ok := semver.SplitPrerelease(relMarker.Version); ok {
				plan.Channel = channel
			}
		}
	case pre != nil:
		if target.Resolver == config.ResolverPython {
			return nil, errors.New(i18n.T("release.err.prerelease_unsupported"))
		}
		plan.Channel = pre.Channel
		switch {
		case len(consumed) == 0:
			plan.Warnings = append(plan.Warnings, i18n.Tf("release.warn.prerelease_idle", pre.Channel))
		case relMarker != nil && markerTagExists && sameChangeSet(d.Root, relMarker, consumed):
			plan.Warnings = append(plan.Warnings, i18n.Tf("release.warn.prerelease_unchanged", relMarker.Version))
		default:
			bump := changes.NextBump(consumed)
			if !bump.Valid() {
				return nil, errors.New("no releasing packages found in the staged change records")
			}
			base, counter, found := channelRelease(tags, d.Cfg.Git.TagPrefix, pre.Channel)
			if !found && pre.Version != "" {
				if lockBase, _, lockCounter, ok := semver.SplitPrerelease(pre.Version); ok {
					base, counter, found = lockBase, lockCounter, true
				}
			}
			next, err := semver.Next(stableBaseVersion(tags, d.Cfg.Git.TagPrefix, target.Version), bump)
			if err != nil {
				return nil, err
			}
			baseVersion := next.String()
			counterNext := 1
			if found {
				if cmp, err := semver.Compare(base, baseVersion); err == nil && cmp >= 0 {
					baseVersion = base
					counterNext = counter + 1
				}
			}
			nextVersion, err := semver.Prerelease(baseVersion, pre.Channel, counterNext)
			if err != nil {
				return nil, err
			}
			if err := versionPlan(ctx, d, plan, pkgName, target, publishable, nextVersion, string(bump), records, repo); err != nil {
				return nil, err
			}
			previousTag()
		}
	case relMarker != nil && markerTagExists && semver.IsPrerelease(relMarker.Version):
		plan.Graduating = true
		preBase, _ := semver.Stable(relMarker.Version)
		nextVersion := preBase
		bump := ""
		if len(consumed) > 0 {
			if recordBump := changes.NextBump(consumed); recordBump.Valid() {
				bump = string(recordBump)
				next, err := semver.Next(stableBaseVersion(tags, d.Cfg.Git.TagPrefix, target.Version), recordBump)
				if err != nil {
					return nil, err
				}
				if cmp, err := semver.Compare(next.String(), preBase); err == nil && cmp > 0 {
					nextVersion = next.String()
				}
			}
		}
		if err := versionPlan(ctx, d, plan, pkgName, target, publishable, nextVersion, bump, records, repo); err != nil {
			return nil, err
		}
		previousTag()
	case len(consumed) > 0:
		bump := changes.NextBump(consumed)
		if !bump.Valid() {
			return nil, errors.New("no releasing packages found in the staged change records")
		}
		next, err := semver.Next(target.Version, bump)
		if err != nil {
			return nil, err
		}
		if err := versionPlan(ctx, d, plan, pkgName, target, publishable, next.String(), string(bump), records, repo); err != nil {
			return nil, err
		}
		previousTag()
	default:
		plan.Phase = PhaseNone
		if relMarker != nil && relMarker.Version != target.Version {
			plan.Warnings = append(plan.Warnings, i18n.T("release.warn.marker_mismatch"))
		}
	}

	plan.Steps = stepsFor(plan.Phase, d.Cfg.Release.Mode, plan.Assets != nil)
	plan.Warnings = append(plan.Warnings, packageWarnings(registered, detected)...)
	if tagVersioned && d.Cfg.Git.TagPrefix != "v" {
		plan.Warnings = append(plan.Warnings, i18n.Tf("release.warn.tag_version_prefix", d.Cfg.Git.TagPrefix))
	}
	if d.Cfg.Release.Mode == config.ModeCommit {
		plan.Warnings = append(plan.Warnings, i18n.T("release.warn.commit_mode"))
	}
	if plan.Phase == PhaseNone {
		plan.Fingerprint = ""
	} else {
		plan.Fingerprint, err = fingerprint(plan)
		if err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func buildChanges(ctx context.Context, d *Deps, records []*changes.Record, repo github.Repo) ([]ChangePlan, error) {
	lookup := newAuthorLookup(ctx, d, repo)
	out := make([]ChangePlan, 0, len(records))
	for _, record := range records {
		bump := ""
		for _, level := range record.Packages {
			if level.Rank() > semver.Bump(bump).Rank() {
				bump = string(level)
			}
		}
		rel := fsx.Rel(d.Root, record.Path)
		commit, err := d.Git.IntroducingCommit(ctx, rel)
		if err != nil {
			commit = ""
		}
		pullNumber := 0
		if commit != "" {
			if subject, err := d.Git.CommitSubject(ctx, commit); err == nil {
				pullNumber = pullRequestNumber(subject)
			}
		}
		author := lookup.resolve(ctx, pullNumber, commit)
		pullURL := ""
		if pullNumber > 0 {
			pullURL = repo.PullURL(pullNumber)
		}
		out = append(out, ChangePlan{
			File:       rel,
			Type:       record.Type,
			Scope:      record.Scope,
			Bump:       bump,
			Body:       record.Body,
			Commit:     commit,
			CommitURL:  repo.CommitURL(commit),
			PullNumber: pullNumber,
			PullURL:    pullURL,
			Author:     author,
		})
	}
	return out, nil
}

func markerPackages(packages []state.MarkerPackage) []PackagePlan {
	out := make([]PackagePlan, 0, len(packages))
	for _, pkg := range packages {
		out = append(out, PackagePlan{
			Name: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver,
			Publish: pkg.Publish, Current: pkg.Current, Next: pkg.Next, Bump: pkg.Bump,
		})
	}
	return out
}

func markerChanges(changes []state.MarkerChange) []ChangePlan {
	out := make([]ChangePlan, 0, len(changes))
	for _, change := range changes {
		out = append(out, ChangePlan{
			File: change.File, Type: change.Type, Scope: change.Scope,
			Bump: change.Bump, Body: change.Body, Commit: change.Commit, CommitURL: change.CommitURL,
			PullNumber: change.PullNumber, PullURL: change.PullURL, Author: change.Author,
		})
	}
	return out
}

func packageWarnings(registered map[string]config.Package, detected []ecosystem.Package) []string {
	var warnings []string
	for _, pkg := range detected {
		if _, ok := registered[pkg.Name]; !ok {
			warnings = append(warnings, i18n.Tf("release.warn.unregistered_package", pkg.Name))
		}
	}
	sort.Strings(warnings)
	return warnings
}

var pullRequestNumberRE = regexp.MustCompile(`\(#(\d+)\)`)

// pullRequestNumber extracts the pull request number a squash commit carries.
func pullRequestNumber(message string) int {
	match := pullRequestNumberRE.FindStringSubmatch(message)
	if len(match) != 2 {
		return 0
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return number
}

// markerApplies reports whether the pending version from the release marker
// still corresponds to the checked out tree. Manifest ecosystems require an
// exact match because the bump commit wrote the version into the manifest;
// tag-versioned ecosystems (Go) keep the previous tag until the pending tag is
// created, so any version at or above the current one applies.
func markerApplies(pending, current string, tagVersioned bool) bool {
	if !tagVersioned {
		return pending == current
	}
	order, err := semver.Compare(pending, current)
	if err != nil {
		return pending == current
	}
	return order >= 0
}

func findDetected(detected []ecosystem.Package, name, pkgPath string) (ecosystem.Package, bool) {
	for _, pkg := range detected {
		if pkg.Name == name {
			return pkg, true
		}
	}
	for _, pkg := range detected {
		if pkg.Path == pkgPath {
			return pkg, true
		}
	}
	if len(detected) == 1 {
		return detected[0], true
	}
	return ecosystem.Package{}, false
}

func changesDir(d *Deps) string {
	dir := d.Cfg.Changes.Dir
	if dir == "" {
		dir = ".changes"
	}
	return fsx.Join(d.Root, dir)
}

func joinRoot(root, rel string) string {
	if rel == "" {
		return root
	}
	return fsx.Join(root, rel)
}

func relativePath(root, target string) string {
	rel, err := filepathRel(root, target)
	if err != nil {
		return target
	}
	return rel
}
