package release

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// target is one registered package matched against a detected manifest.
type target struct {
	name    string
	cfg     config.Package
	pkg     ecosystem.Package
	publish bool
}

func (t target) tagVersioned() bool { return registry.IsTagVersioned(t.pkg.Resolver) }

// releaseUnit is a group of packages that share one version and one tag. A
// shared-version repository has one unit; a package-scoped multi-package
// repository has one unit per releasing package.
type releaseUnit struct {
	name    string
	targets []target
	bump    semver.Bump
	current string
	version string
	tag     string
	prevTag string
}

// Build computes the plan for the current repository state.
func Build(ctx context.Context, d *Deps) (*Plan, error) {
	detected, err := registry.DetectAll(d.Root)
	if err != nil {
		return nil, err
	}
	registered := d.Cfg.Packages
	var inspected []ecosystem.Package
	if len(registered) == 0 {
		if len(detected) != 1 {
			return nil, fmt.Errorf("no packages registered; run qegg init (detected %d packages)", len(detected))
		}
		publishable := detected[0].Publish
		registered = map[string]config.Package{
			detected[0].Name: {Path: detected[0].Path, Resolver: detected[0].Resolver, Publish: &publishable},
		}
		inspected = []ecosystem.Package{detected[0]}
	} else {
		inspected, err = inspectConfigured(d.Root, registered)
		if err != nil {
			return nil, err
		}
	}
	scope := d.Cfg.Release.VersionScope
	if scope == "" {
		scope = config.VersionScopePackage
	}
	targets, err := resolveTargets(ctx, d, inspected, registered)
	if err != nil {
		return nil, err
	}
	if err := validateTargets(scope, targets); err != nil {
		return nil, err
	}

	records, err := changes.LoadDir(changesDir(d))
	if err != nil {
		return nil, err
	}
	names := targetNames(targets)
	for _, record := range records {
		if err := record.Validate(names); err != nil {
			return nil, fmt.Errorf("%s: %w", record.Path, err)
		}
	}
	consumed := changes.Consumed(records)
	bumps := bumpMap(consumed)

	plan := &Plan{
		SchemaVersion: 2,
		Phase:         PhaseNone,
		HasChanges:    len(consumed) > 0,
		VersionScope:  scope,
		Project: ProjectInfo{
			Name:      targets[0].name,
			Ecosystem: targets[0].pkg.Resolver,
			Manifest:  targets[0].pkg.Manifest,
			Root:      d.Root,
		},
		Version: VersionInfo{Current: targets[0].pkg.Version},
		Verify: VerifyInfo{
			Commands: append([]string(nil), d.Cfg.Verify.Commands...),
			Timeout:  d.Cfg.Verify.Timeout,
			Source:   "config",
		},
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

	pre, err := state.LoadPreRelease(d.Root)
	if err != nil {
		return nil, err
	}
	if pre != nil && pre.Version != "" {
		matched := false
		for _, t := range targets {
			_, channel, _, ok := registry.SplitPrereleaseVersion(t.pkg.Resolver, pre.Version)
			if ok && channel == pre.Channel {
				matched = true
				break
			}
		}
		if !matched {
			return nil, errors.New(i18n.Tf("release.err.prerelease_lock_mismatch", pre.Version, pre.Channel))
		}
	}
	if pre == nil {
		for _, t := range targets {
			if _, _, _, ok := registry.SplitPrereleaseVersion(t.pkg.Resolver, t.pkg.Version); ok {
				plan.Warnings = append(plan.Warnings, i18n.T("release.warn.prerelease_unlocked"))
				break
			}
		}
	}

	if err := buildReleases(ctx, d, plan, targets, records, consumed, bumps, relMarker, pre); err != nil {
		return nil, err
	}
	if plan.Phase != PhaseNone {
		assetPlan, assetWarnings, err := planAssets(d, targets)
		if err != nil {
			return nil, err
		}
		plan.Assets = assetPlan
		plan.Warnings = append(plan.Warnings, assetWarnings...)
	}
	if err := validateCommitMessage(d.Cfg, plan); err != nil {
		return nil, err
	}
	if scope != config.VersionScopeShared {
		if inherited := inheritedWorkspaceVersions(inspected); len(inherited) > 1 {
			plan.Warnings = append(plan.Warnings, i18n.Tf("release.err.version_inherited", inherited[0], inherited[1]))
		}
	}

	plan.Steps = stepsFor(plan.Phase, d.Cfg.Release.Mode, plan.Assets != nil || d.Options.AssetsDir != "")
	plan.Warnings = append(plan.Warnings, packageWarnings(registered, detected)...)
	plan.Warnings = append(plan.Warnings, versionFileWarnings(d.Root, d.Cfg.Version.File, plan.Version.Current)...)
	if len(targets) == 1 && targets[0].tagVersioned() {
		if spec, ok := registry.SpecFor(targets[0].pkg.Resolver); ok && spec.TagPrefix != "" && d.Cfg.Git.TagPrefix != spec.TagPrefix {
			plan.Warnings = append(plan.Warnings, i18n.Tf("release.warn.tag_prefix", spec.Language, spec.TagPrefix, d.Cfg.Git.TagPrefix))
		}
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

func resolveTargets(ctx context.Context, d *Deps, inspected []ecosystem.Package, registered map[string]config.Package) ([]target, error) {
	targets := make([]target, 0, len(inspected))
	for _, pkg := range inspected {
		pkgCfg, ok := registered[pkg.Name]
		if !ok {
			pkgCfg = config.Package{Path: pkg.Path, Resolver: pkg.Resolver}
		}
		publishable := pkg.Publish
		if pkgCfg.Publish != nil {
			publishable = *pkgCfg.Publish
		}
		current, err := registry.CurrentVersion(ctx, d.Run, d.Root, pkg, d.Cfg.Git.TagPrefix)
		if err != nil {
			return nil, err
		}
		pkg.Version = current
		targets = append(targets, target{name: pkg.Name, cfg: pkgCfg, pkg: pkg, publish: publishable})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	return targets, nil
}

// inspectConfigured reads every configured package from its path. It is the
// only source of package data once the configuration registers packages.
func inspectConfigured(root string, registered map[string]config.Package) ([]ecosystem.Package, error) {
	names := make([]string, 0, len(registered))
	for name := range registered {
		names = append(names, name)
	}
	sort.Strings(names)
	inputs := make([]ecosystem.Package, 0, len(names))
	for _, name := range names {
		pkgCfg := registered[name]
		inputs = append(inputs, ecosystem.Package{Name: name, Path: pkgCfg.Path, Resolver: pkgCfg.Resolver})
	}
	return registry.InspectAll(root, inputs)
}

func validateTargets(scope string, targets []target) error {
	if len(targets) <= 1 {
		return nil
	}
	for _, t := range targets {
		if t.tagVersioned() {
			return errors.New(i18n.Tf("release.err.multi_package_tag_versioned", t.name))
		}
	}
	if scope != config.VersionScopeShared {
		return nil
	}
	first := targets[0]
	for _, t := range targets[1:] {
		if t.pkg.Version != first.pkg.Version {
			return errors.New(i18n.Tf("release.err.shared_version_mismatch", t.name, t.pkg.Version, first.name, first.pkg.Version))
		}
		// One tag carries the version of every package in the unit, so the
		// ecosystems must spell that version the same way in a tag.
		want := registry.TagVersion(t.pkg.Resolver, t.pkg.Version)
		have := registry.TagVersion(first.pkg.Resolver, first.pkg.Version)
		if want != have {
			return errors.New(i18n.Tf("release.err.shared_tag_spelling", t.name, want, first.name, have))
		}
	}
	return nil
}

func buildReleases(ctx context.Context, d *Deps, plan *Plan, targets []target, records, consumed []*changes.Record, bumps map[string]semver.Bump, relMarker *state.Marker, pre *state.PreRelease) error {
	tags, err := d.Git.ListTags(ctx, d.Cfg.Git.TagPrefix)
	if err != nil {
		return err
	}
	scope := plan.VersionScope

	if relMarker != nil {
		pending := pendingMarkerReleases(ctx, d, relMarker, targets)
		if len(pending) > 0 {
			plan.Phase = PhasePublish
			plan.Releases = pending
			plan.Packages = markerPackages(relMarker.Packages)
			plan.Changes = markerChanges(relMarker.Changes)
			plan.Channel = relMarker.Channel
			if plan.Channel == "" {
				for _, release := range pending {
					if release.Prerelease != "" {
						plan.Channel = release.Prerelease
						break
					}
				}
			}
			if relMarker.Version != "" {
				plan.Version = VersionInfo{Current: targets[0].pkg.Version, Next: relMarker.Version}
				if len(pending) == 1 {
					plan.Tag = pending[0].Tag
					plan.PreviousTag = pending[0].PrevTag
				}
			}
			return nil
		}
	}

	if pre != nil {
		plan.Channel = pre.Channel
		units := releaseUnits(scope, targets, bumps, consumed)
		if len(consumed) == 0 || len(units) == 0 {
			plan.Warnings = append(plan.Warnings, i18n.Tf("release.warn.prerelease_idle", pre.Channel))
			return nil
		}
		if relMarker != nil && allMarkerTagsExist(ctx, d, relMarker, targets) && sameChangeSet(d.Root, relMarker, consumed) {
			plan.Warnings = append(plan.Warnings, i18n.Tf("release.warn.prerelease_unchanged", relMarker.Version))
			return nil
		}
		for i := range units {
			units[i].current = unitCurrent(units[i])
			next, err := nextPrereleaseVersion(d, plan, units[i], pre, tags)
			if err != nil {
				return err
			}
			units[i].version = next
			tag, err := packageTag(d.Cfg, scope, units[i].targets[0].pkg, next)
			if err != nil {
				return err
			}
			units[i].tag = tag
			units[i].prevTag = previousTag(ctx, d, scope, units[i])
		}
		return applyUnits(ctx, d, plan, targets, units, records)
	}

	if relMarker != nil {
		units := graduationUnits(ctx, d, plan, targets, bumps, consumed, relMarker, tags)
		if len(units) > 0 {
			plan.Graduating = true
			return applyUnits(ctx, d, plan, targets, units, records)
		}
	}

	units := releaseUnits(scope, targets, bumps, consumed)
	if len(units) == 0 {
		plan.Phase = PhaseNone
		if relMarker != nil && relMarker.Version != "" && relMarker.Version != plan.Version.Current {
			plan.Warnings = append(plan.Warnings, i18n.T("release.warn.marker_mismatch"))
		}
		return nil
	}
	for i := range units {
		units[i].current = unitCurrent(units[i])
		next, err := semver.Next(units[i].current, units[i].bump)
		if err != nil {
			return err
		}
		units[i].version = next.String()
		tag, err := packageTag(d.Cfg, scope, units[i].targets[0].pkg, units[i].version)
		if err != nil {
			return err
		}
		units[i].tag = tag
		units[i].prevTag = previousTag(ctx, d, scope, units[i])
	}
	return applyUnits(ctx, d, plan, targets, units, records)
}

// releaseUnits lists the units that have work to do in the current scope.
func releaseUnits(scope string, targets []target, bumps map[string]semver.Bump, consumed []*changes.Record) []releaseUnit {
	if scope == config.VersionScopeShared {
		bump := changes.NextBump(consumed)
		if !bump.Valid() {
			return nil
		}
		return []releaseUnit{{targets: targets, bump: bump}}
	}
	units := make([]releaseUnit, 0, len(targets))
	for _, t := range targets {
		bump := bumps[t.name]
		if !bump.Valid() {
			continue
		}
		units = append(units, releaseUnit{name: t.name, targets: []target{t}, bump: bump})
	}
	return units
}

func nextPrereleaseVersion(d *Deps, plan *Plan, unit releaseUnit, pre *state.PreRelease, tags []string) (string, error) {
	resolver := unit.targets[0].pkg.Resolver
	versions := unitTagVersions(tags, d.Cfg, plan.VersionScope, unit)
	base, counter, found := channelReleaseVersions(versions, resolver, pre.Channel)
	if !found && pre.Version != "" {
		if lockBase, _, lockCounter, ok := registry.SplitPrereleaseVersion(resolver, pre.Version); ok {
			base, counter, found = lockBase, lockCounter, true
		}
	}
	next, err := semver.Next(stableBaseVersions(versions, resolver, unit.current), unit.bump)
	if err != nil {
		return "", err
	}
	baseVersion := next.String()
	counterNext := 1
	if found {
		if cmp, err := semver.Compare(base, baseVersion); err == nil && cmp >= 0 {
			baseVersion = base
			counterNext = counter + 1
		}
	} else if currentBase, _, currentCounter, ok := registry.SplitPrereleaseVersion(resolver, unit.current); ok {
		if cmp, err := semver.Compare(currentBase, baseVersion); err == nil && cmp >= 0 {
			baseVersion = currentBase
			counterNext = currentCounter + 1
		}
	}
	return registry.PrereleaseVersion(resolver, baseVersion, pre.Channel, counterNext)
}

func graduationUnits(ctx context.Context, d *Deps, plan *Plan, targets []target, bumps map[string]semver.Bump, consumed []*changes.Record, marker *state.Marker, tags []string) []releaseUnit {
	units := []releaseUnit{}
	for _, release := range markerReleasePlans(d, marker, targets) {
		exists, err := d.Git.TagExists(ctx, release.Tag)
		if err != nil || !exists {
			continue
		}
		if !semver.IsPrerelease(release.Version) {
			continue
		}
		unitTargets := make([]target, 0, len(release.Packages))
		for _, name := range release.Packages {
			if t := findTarget(targets, name); t != nil {
				unitTargets = append(unitTargets, *t)
			}
		}
		if len(unitTargets) == 0 {
			continue
		}
		current := unitTargets[0].pkg.Version
		bump := semver.Bump("")
		if plan.VersionScope == config.VersionScopeShared || len(unitTargets) > 1 {
			bump = changes.NextBump(consumed)
		} else {
			bump = bumps[unitTargets[0].name]
		}
		preBase, _ := semver.Stable(release.Version)
		nextVersion := preBase
		if bump.Valid() {
			versions := unitTagVersions(tags, d.Cfg, plan.VersionScope, releaseUnit{name: release.Name, targets: unitTargets})
			candidate, err := semver.Next(stableBaseVersions(versions, unitTargets[0].pkg.Resolver, current), bump)
			if err == nil {
				if cmp, compareErr := semver.Compare(candidate.String(), preBase); compareErr == nil && cmp > 0 {
					nextVersion = candidate.String()
				}
			}
		}
		tag, err := packageTag(d.Cfg, plan.VersionScope, unitTargets[0].pkg, nextVersion)
		if err != nil {
			continue
		}
		units = append(units, releaseUnit{
			name: release.Name, targets: unitTargets, bump: bump, current: current,
			version: nextVersion, tag: tag, prevTag: release.Tag,
		})
	}
	return units
}

func applyUnits(ctx context.Context, d *Deps, plan *Plan, targets []target, units []releaseUnit, records []*changes.Record) error {
	usedTags := map[string]string{}
	for _, unit := range units {
		if previous, ok := usedTags[unit.tag]; ok {
			return errors.New(i18n.Tf("release.err.tag_collision", unit.tag, previous, unit.name))
		}
		usedTags[unit.tag] = unit.name
		for _, t := range unit.targets {
			if err := registry.CheckVersion(t.pkg, unit.version); err != nil {
				return err
			}
		}
	}
	released := map[string]bool{}
	for _, unit := range units {
		for _, t := range unit.targets {
			released[t.name] = true
		}
	}
	selected := make([]*changes.Record, 0, len(records))
	for _, record := range records {
		include := true
		if plan.VersionScope == config.VersionScopePackage && len(targets) > 1 {
			include = false
			for name := range record.Packages {
				if released[name] {
					include = true
					break
				}
			}
		}
		if include {
			selected = append(selected, record)
		}
	}
	planned, err := buildChanges(ctx, d, selected, plan.Repo)
	if err != nil {
		return err
	}
	plan.Changes = planned

	releaseByName := map[string]ReleasePlan{}
	plan.Releases = make([]ReleasePlan, 0, len(units))
	for _, unit := range units {
		prerelease := ""
		if plan.Channel != "" {
			prerelease = plan.Channel
		}
		name := unit.name
		if len(targets) == 1 || plan.VersionScope == config.VersionScopeShared {
			name = ""
		}
		release := ReleasePlan{
			Name:       name,
			Version:    unit.version,
			Tag:        unit.tag,
			PrevTag:    unit.prevTag,
			Packages:   targetNames(unit.targets),
			Prerelease: prerelease,
		}
		plan.Releases = append(plan.Releases, release)
		for _, name := range release.Packages {
			releaseByName[name] = release
		}
	}

	plan.Packages = make([]PackagePlan, 0, len(targets))
	for _, t := range orderTargets(targets, d.Cfg.Packages) {
		pkg := PackagePlan{
			Name: t.name, Path: t.pkg.Path, Resolver: t.pkg.Resolver,
			Publish: t.publish, Current: t.pkg.Version, Manifest: t.pkg.Manifest,
		}
		if release, ok := releaseByName[t.name]; ok {
			pkg.Next = release.Version
			pkg.Tag = release.Tag
			pkg.Bump = string(unitBump(units, t.name))
		} else {
			pkg.Next = t.pkg.Version
		}
		plan.Packages = append(plan.Packages, pkg)
	}

	if plan.VersionScope != config.VersionScopePackage || len(targets) == 1 {
		if len(units) == 1 {
			plan.Version = VersionInfo{Current: units[0].current, Next: units[0].version, Bump: string(units[0].bump)}
			plan.Tag = units[0].tag
			plan.PreviousTag = units[0].prevTag
		}
	}
	plan.Phase = PhaseVersion
	return nil
}

func unitBump(units []releaseUnit, name string) semver.Bump {
	for _, unit := range units {
		for _, t := range unit.targets {
			if t.name == name {
				return unit.bump
			}
		}
	}
	return ""
}

func unitCurrent(unit releaseUnit) string {
	if len(unit.targets) == 0 {
		return ""
	}
	return unit.targets[0].pkg.Version
}

func targetNames(targets []target) []string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.name)
	}
	sort.Strings(names)
	return names
}

// inheritedWorkspaceVersions lists detected packages whose version comes from
// the workspace manifest rather than from their own manifest.
func inheritedWorkspaceVersions(detected []ecosystem.Package) []string {
	var names []string
	for _, pkg := range detected {
		if pkg.Inherited {
			names = append(names, pkg.Name)
		}
	}
	sort.Strings(names)
	return names
}

// orderTargets sorts targets so a package precedes everything that depends on
// it, keeping the alphabetical order for independent packages. Dependencies
// come from the manifests and from the configured depends-on list. A cycle
// (impossible in Cargo, defensively handled) leaves the rest untouched.
func orderTargets(targets []target, registered map[string]config.Package) []target {
	if len(targets) < 2 {
		return targets
	}
	index := make(map[string]int, len(targets))
	for i, t := range targets {
		index[t.name] = i
	}
	dependents := make([][]int, len(targets))
	indegree := make([]int, len(targets))
	for j, t := range targets {
		deps := append([]string(nil), t.pkg.Deps...)
		if cfg, ok := registered[t.name]; ok {
			deps = append(deps, cfg.DependsOn...)
		}
		for _, dep := range deps {
			i, ok := dependencyTarget(index, dep)
			if !ok || i == j {
				continue
			}
			dependents[i] = append(dependents[i], j)
			indegree[j]++
		}
	}
	ordered := make([]target, 0, len(targets))
	used := make([]bool, len(targets))
	for len(ordered) < len(targets) {
		next := -1
		for i := range targets {
			if !used[i] && indegree[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			for i := range targets {
				if !used[i] {
					ordered = append(ordered, targets[i])
					used[i] = true
				}
			}
			break
		}
		used[next] = true
		ordered = append(ordered, targets[next])
		for _, j := range dependents[next] {
			indegree[j]--
		}
	}
	return ordered
}

// dependencyTarget resolves a dependency name to a target, tolerating the
// dash/underscore spelling difference between crate names and code identifiers.
func dependencyTarget(index map[string]int, name string) (int, bool) {
	if i, ok := index[name]; ok {
		return i, true
	}
	for _, candidate := range []string{strings.ReplaceAll(name, "_", "-"), strings.ReplaceAll(name, "-", "_")} {
		if i, ok := index[candidate]; ok {
			return i, true
		}
	}
	return 0, false
}

func findTarget(targets []target, name string) *target {
	for i := range targets {
		if targets[i].name == name {
			return &targets[i]
		}
	}
	return nil
}

func bumpMap(consumed []*changes.Record) map[string]semver.Bump {
	out := map[string]semver.Bump{}
	for _, record := range consumed {
		for name, bump := range record.Packages {
			out[name] = semver.Max(out[name], bump)
		}
	}
	return out
}

func previousTag(ctx context.Context, d *Deps, scope string, unit releaseUnit) string {
	tag, err := packageTag(d.Cfg, scope, unit.targets[0].pkg, unit.current)
	if err != nil {
		return ""
	}
	exists, err := d.Git.TagExists(ctx, tag)
	if err == nil && exists {
		return tag
	}
	return ""
}

func markerReleasePlans(d *Deps, marker *state.Marker, targets []target) []ReleasePlan {
	if len(marker.Releases) > 0 {
		out := make([]ReleasePlan, 0, len(marker.Releases))
		for _, release := range marker.Releases {
			out = append(out, ReleasePlan{
				Name: release.Name, Version: release.Version, Tag: release.Tag,
				PrevTag: release.PrevTag, Packages: append([]string(nil), release.Packages...),
				Prerelease: release.Prerelease,
			})
		}
		return out
	}
	if marker.Version == "" {
		return nil
	}
	resolver := ""
	packages := make([]string, 0, len(marker.Packages))
	for _, pkg := range marker.Packages {
		if resolver == "" {
			resolver = pkg.Resolver
		}
		packages = append(packages, pkg.Name)
	}
	if resolver == "" && len(targets) > 0 {
		resolver = targets[0].pkg.Resolver
	}
	if len(packages) == 0 {
		for _, t := range targets {
			packages = append(packages, t.name)
		}
	}
	return []ReleasePlan{{
		Version:    marker.Version,
		Tag:        tagName(d.Cfg, resolver, marker.Version),
		Packages:   packages,
		Prerelease: marker.Channel,
	}}
}

func pendingMarkerReleases(ctx context.Context, d *Deps, marker *state.Marker, targets []target) []ReleasePlan {
	pending := []ReleasePlan{}
	for _, release := range markerReleasePlans(d, marker, targets) {
		exists, err := d.Git.TagExists(ctx, release.Tag)
		if err != nil || exists {
			continue
		}
		applies := len(release.Packages) > 0
		for _, name := range release.Packages {
			t := findTarget(targets, name)
			if t == nil || !markerApplies(release.Version, t.pkg.Version, t.tagVersioned()) {
				applies = false
				break
			}
		}
		if applies {
			pending = append(pending, release)
		}
	}
	return pending
}

func allMarkerTagsExist(ctx context.Context, d *Deps, marker *state.Marker, targets []target) bool {
	releases := markerReleasePlans(d, marker, targets)
	if len(releases) == 0 {
		return false
	}
	for _, release := range releases {
		exists, err := d.Git.TagExists(ctx, release.Tag)
		if err != nil || !exists {
			return false
		}
	}
	return true
}

func validateCommitMessage(cfg *config.Config, plan *Plan) error {
	if plan.Phase == PhaseNone {
		return nil
	}
	versions := map[string]bool{}
	for _, release := range plan.Releases {
		versions[release.Version] = true
	}
	if len(versions) > 1 && strings.Contains(cfg.Release.CommitMessage, "{{version}}") {
		return errors.New(i18n.T("release.err.commit_message_multi"))
	}
	return nil
}

func releaseCommitMessage(cfg *config.Config, plan *Plan) string {
	message := cfg.Release.CommitMessage
	released := make([]string, 0, len(plan.Releases))
	for _, release := range plan.Releases {
		if release.Name != "" {
			released = append(released, release.Name+"@"+release.Tag)
		} else {
			released = append(released, "v"+release.Version)
		}
	}
	sort.Strings(released)
	first := ""
	if len(plan.Releases) > 0 {
		first = plan.Releases[0].Version
	}
	message = strings.ReplaceAll(message, "{{version}}", first)
	message = strings.ReplaceAll(message, "{{versions}}", strings.Join(released, ", "))
	message = strings.ReplaceAll(message, "{{count}}", strconv.Itoa(len(plan.Releases)))
	return message
}

func buildChanges(ctx context.Context, d *Deps, records []*changes.Record, repo github.Repo) ([]ChangePlan, error) {
	lookup := newAuthorLookup(ctx, d, repo)
	out := make([]ChangePlan, 0, len(records))
	for _, record := range records {
		bump := ""
		names := make([]string, 0, len(record.Packages))
		for name, level := range record.Packages {
			names = append(names, name)
			if level.Rank() > semver.Bump(bump).Rank() {
				bump = string(level)
			}
		}
		sort.Strings(names)
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
			Packages:   names,
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
			Tag: pkg.Tag,
		})
	}
	return out
}

func markerChanges(changes []state.MarkerChange) []ChangePlan {
	out := make([]ChangePlan, 0, len(changes))
	for _, change := range changes {
		out = append(out, ChangePlan{
			File: change.File, Type: change.Type, Scope: change.Scope,
			Bump: change.Bump, Packages: append([]string(nil), change.Packages...), Body: change.Body,
			Commit: change.Commit, CommitURL: change.CommitURL,
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

func changesDir(d *Deps) string {
	dir := d.Cfg.Changes.Dir
	if dir == "" {
		dir = ".changes"
	}
	return fsx.Join(d.Root, dir)
}

func relativePath(root, target string) string {
	rel, err := filepathRel(root, target)
	if err != nil {
		return target
	}
	return rel
}
