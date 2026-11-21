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
	"os/exec"
	"path/filepath"
)

func (e *executor) runPublish(ctx context.Context) error {
	if err := e.step(ctx, "preflight", e.preflightPublish); err != nil {
		return err
	}
	// The release snapshot is pinned before anything is built, uploaded or tagged,
	// and before the ledger names the source: a branch that moved on must not
	// reach the release.
	if err := e.step(ctx, "source", e.resolveSource); err != nil {
		return err
	}
	// The ledger is opened before anything leaves the machine: a release that
	// cannot record what it is about to do must not do it.
	session, err := e.openLedger(ctx, false)
	if err != nil {
		return err
	}
	defer session.Close()
	e.ledger = session
	if err := e.recordRound(); err != nil {
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

// recordRound persists the decisions that apply to the whole release, so a later
// resume repeats them without the flags.
func (e *executor) recordRound() error {
	decisions := state.RoundDecisions{
		NoPush:          e.d.Options.NoPush,
		NoGitHubRelease: e.d.Options.NoGitHubRelease,
		NoAssets:        e.d.Options.NoAssets,
		ReleaseEnabled:  e.d.Cfg.Release.GitHubRelease,
		Store:           e.ledger.kind,
	}
	if e.d.Options.NoPublish {
		decisions.NoPublish = state.ReasonNoPublish
	}
	return e.ledger.recordRound(decisions)
}

// syncReleaseBranch flattens the release branch onto the published commit once
// the release is out, so an idle branch never lags behind main. A branch that
// GitHub already deleted stays deleted and is recreated by the next release.
func (e *executor) syncReleaseBranch(ctx context.Context) error {
	if e.noPush() {
		e.skip("sync", i18n.T("release.skip.no_push"))
		return e.ledger.step("sync", state.StatusSkipped, state.ReasonNoPush)
	}
	branch := e.releaseBranch()
	exists, err := e.d.Git.RemoteBranchExists(ctx, e.d.Cfg.Git.Remote, branch)
	if err != nil {
		e.d.UI.Warn("%s", i18n.Tf("release.warn.sync_failed", err))
		return e.ledger.step("sync", state.StatusSkipped, "")
	}
	if !exists {
		return e.ledger.step("sync", state.StatusSkipped, "")
	}
	source := e.identity.SourceCommit
	if source == "" {
		// A run that never pinned a snapshot must not flatten the branch onto
		// whatever happens to be checked out.
		e.d.UI.Warn("%s", i18n.T("release.err.source_unset"))
		return e.ledger.step("sync", state.StatusSkipped, "")
	}
	// Flatten the branch onto the published commit, never onto HEAD: a resume
	// after the branch moved on must keep the release branch on the release.
	if err := e.d.Git.Push(ctx, e.d.Cfg.Git.Remote, releaseRefspecAt(source, branch)); err != nil {
		e.d.UI.Warn("%s", i18n.Tf("release.warn.sync_failed", err))
		return e.ledger.step("sync", state.StatusSkipped, "")
	}
	return e.ledger.step("sync", state.StatusDone, "")
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

// publishPackages decides per package whether an upload is still needed. Every
// decision is written to the ledger, and a write that fails stops the release
// before the upload it was about to describe.
func (e *executor) publishPackages(ctx context.Context) error {
	if e.ledger == nil {
		return errors.New(i18n.T("release.err.ledger_closed"))
	}
	if reason := e.publishSkipReason(); reason != "" {
		e.skip("publish", reasonText(reason))
		// Every package keeps the reason too: a later resume has to see why the
		// release left no artefact, not only that the step was skipped.
		for _, pkg := range e.plan.Packages {
			if !pkg.Publish {
				continue
			}
			if err := e.ledger.finish(pkg, e.packageTarget(pkg), state.OutcomeSkipped, reason); err != nil {
				return err
			}
			e.setPackageState(pkg.Name, state.StatusSkipped, reason)
		}
		return e.ledger.step("publish", state.StatusSkipped, reason)
	}
	for _, pkg := range e.plan.Packages {
		if err := e.publishOne(ctx, pkg); err != nil {
			return err
		}
	}
	return e.ledger.step("publish", state.StatusDone, "")
}

// publishSkipReason returns the recorded reason an upload is skipped for the
// whole release, if any.
func (e *executor) publishSkipReason() string {
	if e.d.Options.NoPublish {
		return state.ReasonNoPublish
	}
	return e.ledger.round().NoPublish
}

func (e *executor) publishOne(ctx context.Context, pkg PackagePlan) error {
	target := e.packageTarget(pkg)
	if !pkg.Publish {
		return e.ledger.finish(pkg, target, state.OutcomeSkipped, state.ReasonPublishFalse)
	}
	if reason := e.publishSkipReason(); reason != "" {
		return e.ledger.finish(pkg, target, state.OutcomeSkipped, reason)
	}
	facts := e.packageFacts(ctx, pkg, target)
	decision := decidePackage(facts)
	switch decision.action {
	case actionStop:
		return decisionError(pkg, target, facts, decision)
	case actionSkip:
		e.reportSkip(pkg, decision)
		if decision.record {
			return e.ledger.finish(pkg, target, state.OutcomeUploaded, decision.reason)
		}
		return nil
	case actionDefer:
		e.d.UI.Info("%s", i18n.Tf("release.msg.tag_publishes", pkg.Name, pkg.Next))
		return e.ledger.finish(pkg, target, state.OutcomeStarted, decision.reason)
	}
	return e.uploadPackage(ctx, pkg, target)
}

// reportSkip tells the user why a package needs no upload.
func (e *executor) reportSkip(pkg PackagePlan, decision packageDecision) {
	switch decision.reason {
	case state.ReasonExists:
		e.d.UI.Info("%s", i18n.Tf("release.msg.registry_exists", pkg.Name, pkg.Next))
	case state.ReasonTagPublished:
		e.d.UI.Info("%s", i18n.Tf("release.msg.tag_published", pkg.Name, pkg.Next))
	case state.ReasonPublished, "":
		e.d.UI.Info("%s", i18n.Tf("release.msg.ledger_done", pkg.Name, pkg.Next))
	default:
		e.d.UI.Info("%s", i18n.Tf("release.msg.publish_skipped", pkg.Name, reasonText(decision.reason)))
	}
}

// uploadPackage records the attempt, publishes and records the result. The
// attempt is written before the request leaves the machine.
func (e *executor) uploadPackage(ctx context.Context, pkg PackagePlan, target string) error {
	if err := e.ledger.begin(pkg, target); err != nil {
		return err
	}
	input := e.packageInput(pkg)
	// Build and publish from the pinned snapshot, never from a working tree that
	// may have moved on since the release was prepared.
	root, err := e.sourceRoot(ctx)
	if err != nil {
		return err
	}
	if err := registry.Build(ctx, e.d.Run, root, input); err != nil {
		// The build runs before the publish command, so the upload never started.
		return e.recordFailure(pkg, target, false, err)
	}
	if err := registry.Publish(ctx, e.d.Run, root, input, pkg.Next); err != nil {
		return e.recordFailure(pkg, target, dispatchUnknown(err), err)
	}
	if err := e.ledger.finish(pkg, target, state.OutcomeUploaded, state.ReasonPublished); err != nil {
		return err
	}
	e.setPackageState(pkg.Name, state.StatusDone, "")
	e.d.UI.Success("%s", i18n.Tf("release.msg.published", pkg.Name, pkg.Next))
	return nil
}

// recordFailure records a failed attempt and reports both failures. The record of
// an attempt is key state: if it cannot be written the release has to stop instead
// of leaving a package that looks untouched.
func (e *executor) recordFailure(pkg PackagePlan, target string, dispatched bool, cause error) error {
	if err := e.ledger.fail(pkg, target, dispatched, cause); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// dispatchUnknown reports whether an upload error may have reached the target.
// Only a command that never started is provably un-dispatched, and every other
// failure has to be treated as "the request may have landed".
func dispatchUnknown(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

// packageFacts collects the evidence the decision rules may use. A package whose
// record already reached a terminal state is not queried again, so resuming a
// finished release stays offline and write-free.
func (e *executor) packageFacts(ctx context.Context, pkg PackagePlan, target string) packageFacts {
	facts := packageFacts{record: e.ledger.packageRecord(pkg.Name), tag: e.packageTag(pkg)}
	facts.tagPublished = registry.PublishesByTag(pkg.Resolver)
	facts.ledgerAware = e.plan.Marker != nil && e.plan.Marker.SchemaVersion >= state.MarkerSchemaVersion
	if facts.record != nil {
		switch facts.record.Outcome {
		case state.OutcomeUploaded, state.OutcomeSkipped:
			return facts
		}
	}
	if !facts.tagPublished {
		class, err := registry.Query(ctx, e.d.HTTP, e.packageInput(pkg), pkg.Next)
		facts.class = class
		facts.queryErr = err
		if class != registry.PrecheckUnknown {
			return facts
		}
		// Without a query capability the release tag is the only remaining
		// evidence: a tag that is already on the remote proves the release moved
		// past its uploads, so an unrecorded publish must not deploy again.
	}
	answer, err := readRemoteTag(ctx, e.d.Run, e.d.Root, e.d.Cfg.Git.Remote, facts.tag)
	if err != nil {
		facts.class = registry.PrecheckFailed
		facts.queryErr = err
		return facts
	}
	facts.remoteTag = answer
	facts.remoteTagRead = true
	facts.tagMatches = answer.Present && sameCommit(answer.Commit, e.identity.SourceCommit)
	return facts
}

// packageInput describes pkg for the ecosystem adapters.
func (e *executor) packageInput(pkg PackagePlan) ecosystem.Package {
	manifestName := pkg.Name
	if cfgPkg, ok := e.d.Cfg.Packages[pkg.Name]; ok {
		manifestName = config.ManifestNameOf(pkg.Name, cfgPkg)
	}
	return ecosystem.Package{Name: manifestName, Identity: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver}
}

// packageTarget names the registry a package uploads to in the ledger.
func (e *executor) packageTarget(pkg PackagePlan) string {
	return registry.PublishTarget(pkg.Resolver)
}

// packageTag returns the release tag that publishes pkg.
func (e *executor) packageTag(pkg PackagePlan) string {
	if pkg.Tag != "" {
		return pkg.Tag
	}
	for _, release := range e.plan.Releases {
		for _, name := range release.Packages {
			if name == pkg.Name {
				return release.Tag
			}
		}
	}
	if len(e.plan.Releases) == 1 {
		return e.plan.Releases[0].Tag
	}
	return ""
}

// setPackageState mirrors the ledger into the local run record so qegg status can
// show it. The ledger is authoritative: a local write failure is reported but
// does not undo an upload that already happened.
func (e *executor) setPackageState(name, status, reason string) {
	if e.run.Publish == nil {
		e.run.Publish = map[string]state.PackageState{}
	}
	e.run.Publish[name] = state.PackageState{Status: status, Reason: reason}
	if err := state.SaveRun(e.d.Root, e.run); err != nil {
		e.d.UI.Warn("%s", i18n.Tf("release.warn.run_record", err))
	}
}

func (e *executor) tag(ctx context.Context) error {
	for _, release := range e.plan.Releases {
		exists, err := e.d.Git.TagExists(ctx, release.Tag)
		if err != nil {
			return err
		}
		if exists {
			points, err := e.d.Git.TagPointsAt(ctx, release.Tag, e.identity.SourceCommit)
			if err != nil {
				return err
			}
			if !points {
				return errors.New(i18n.Tf("release.err.tag_mismatch_source", release.Tag, shortSHA(e.identity.SourceCommit)))
			}
			e.skip("tag", i18n.T("release.skip.tag_exists"))
			continue
		}
		if err := e.d.Git.CreateTagAt(ctx, release.Tag, "Release "+release.Tag, e.identity.SourceCommit); err != nil {
			return err
		}
		e.d.UI.Success("%s", i18n.Tf("release.msg.tag_created", release.Tag))
	}
	return e.ledger.step("tag", state.StatusDone, "")
}

// push pushes the branch and then only the tags the remote is missing. A remote
// tag that already points at another commit stops the release; an unchanged
// reference is left alone.
func (e *executor) push(ctx context.Context) error {
	if e.noPush() {
		e.skip("push", i18n.T("release.skip.no_push"))
		return e.ledger.step("push", state.StatusSkipped, state.ReasonNoPush)
	}
	var refs []string
	if e.d.Cfg.Release.Mode != config.ModePR {
		branch, err := e.d.Git.Branch(ctx)
		if err != nil {
			return err
		}
		refs = append(refs, "HEAD:refs/heads/"+branch)
	}
	if len(refs) > 0 {
		if err := e.d.Git.Push(ctx, e.d.Cfg.Git.Remote, refs...); err != nil {
			return err
		}
	}
	verified := map[string]bool{}
	for _, release := range e.plan.Releases {
		answer, err := readRemoteTag(ctx, e.d.Run, e.d.Root, e.d.Cfg.Git.Remote, release.Tag)
		if err != nil {
			return err
		}
		if answer.Present {
			if e.identity.SourceCommit != "" && !sameCommit(answer.Commit, e.identity.SourceCommit) {
				return errors.New(i18n.Tf("release.err.tag_conflict", release.Tag, shortSHA(answer.Commit), shortSHA(e.identity.SourceCommit)))
			}
			verified[release.Tag] = true
			e.d.UI.Info("%s", i18n.Tf("release.msg.tag_in_sync", release.Tag))
			continue
		}
		if err := pushTag(ctx, e.d.Run, e.d.Root, e.d.Cfg.Git.Remote, release.Tag); err != nil {
			return err
		}
		verified[release.Tag] = true
		e.d.UI.Success("%s", i18n.Tf("release.msg.tag_pushed", release.Tag))
	}
	if err := e.completeTaggedPackages(verified); err != nil {
		return err
	}
	return e.ledger.step("push", state.StatusDone, "")
}

// completeTaggedPackages marks the packages whose publication is a release tag
// as published once that tag is on the remote.
func (e *executor) completeTaggedPackages(verified map[string]bool) error {
	for _, pkg := range e.plan.Packages {
		if !registry.PublishesByTag(pkg.Resolver) {
			continue
		}
		record := e.ledger.packageRecord(pkg.Name)
		if record == nil || record.Outcome != state.OutcomeStarted {
			continue
		}
		if !verified[e.packageTag(pkg)] {
			continue
		}
		if err := e.ledger.finish(pkg, e.packageTarget(pkg), state.OutcomeUploaded, state.ReasonTagPublished); err != nil {
			return err
		}
		e.setPackageState(pkg.Name, state.StatusDone, state.ReasonTagPublished)
	}
	return nil
}

// assetsSettled reports whether the attachments of this release are already in
// place: they were built and no later step still has to publish them. A resume
// must not rebuild them, because the rebuild would read the current tree.
func (e *executor) assetsSettled() bool {
	if e.ledger.stepStatus("assets") != state.StatusDone {
		return false
	}
	if e.attachmentsUploaded() {
		return true
	}
	return !e.d.Cfg.Release.GitHubRelease || e.noRelease()
}

// attachmentsUploaded reports whether an earlier run finished the GitHub Release
// step, which is what uploads the attachments.
func (e *executor) attachmentsUploaded() bool {
	status := e.ledger.stepStatus("github-release")
	return status == state.StatusDone || status == state.StatusSkipped
}

// noPush reports whether this run may push, honouring the decision of the run
// that started the release.
func (e *executor) noPush() bool {
	return e.d.Options.NoPush || e.ledger.round().NoPush
}

// noRelease reports whether this run may create a GitHub Release.
func (e *executor) noRelease() bool {
	return e.d.Options.NoGitHubRelease || e.ledger.round().NoGitHubRelease
}

func (e *executor) githubRelease(ctx context.Context) error {
	if e.resumedWithoutRecord {
		// This runner never ran the release and the marker says it is tagged:
		// publishing would be a guess, so it stops and points at qegg release.
		e.d.UI.Info("%s", i18n.T("release.stop.no_record_evidence"))
		e.skip("github-release", i18n.T("release.stop.no_record_evidence"))
		return e.ledger.step("github-release", state.StatusSkipped, state.ReasonReleaseOff)
	}
	if e.releaseRepaired() {
		// The release was already repaired by an earlier resume: creating it again
		// would write to the remote a second time.
		e.skip("github-release", i18n.T("release.skip.release_repaired"))
		return e.ledger.step("github-release", state.StatusDone, "")
	}
	if !e.d.Cfg.Release.GitHubRelease {
		e.skip("github-release", i18n.T("release.skip.github_disabled"))
		e.warnAssetsNotUploaded()
		return e.ledger.step("github-release", state.StatusSkipped, state.ReasonReleaseOff)
	}
	if e.noRelease() {
		e.skip("github-release", i18n.T("release.skip.no_release"))
		e.warnAssetsNotUploaded()
		return e.ledger.step("github-release", state.StatusSkipped, state.ReasonNoRelease)
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
	if len(e.run.Assets) > 0 && len(e.plan.Releases) == 1 && !e.attachmentsUploaded() {
		tag := e.plan.Releases[0].Tag
		if err := ops.UploadReleaseAssets(ctx, e.plan.Repo, tag, e.run.Assets, true); err != nil {
			return err
		}
		e.d.UI.Success("%s", i18n.Tf("release.msg.assets_uploaded", len(e.run.Assets), tag))
	}
	return e.ledger.step("github-release", state.StatusDone, "")
}

// releaseRepaired reports whether an earlier resume already created this
// release. The repair is recorded on the run it happened in, which is the run
// latest.json points at; the publish phase of a later resume starts a new run
// record and must still see it, or it would create the release a second time.
func (e *executor) releaseRepaired() bool {
	if e.run != nil && e.run.Steps["github-release-repair"].Status == state.StatusDone {
		return true
	}
	latest, err := state.LatestRun(e.d.Root)
	return err == nil && latest != nil && latest.Steps["github-release-repair"].Status == state.StatusDone
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
	// The notes come from the pinned snapshot, never from whatever the working
	// tree holds now.
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