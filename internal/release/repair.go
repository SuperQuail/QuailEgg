package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// This file carries the repair path a resume takes when the recorded publish run
// lost only its GitHub Release. Verification and writing are two separate passes:
// every remote tag, every release lookup and every expected digest is checked
// first, and only then is anything created or uploaded. A conflict or an
// unreadable answer therefore leaves the remote untouched, even when the marker
// describes several releases.

// stepGitHubReleaseRepair records that a resume finished the GitHub Release of
// the run. It is separate from the "github-release" publish step so a later
// resume can tell a repaired run from one the original run completed: only the
// repaired run stops before planning the next version.
const stepGitHubReleaseRepair = "github-release-repair"

// ReleaseOps is the narrow GitHub release surface the release executor needs. It
// lets a repair be driven by a fake that counts external writes; the default
// implementation talks to GitHub through the gh CLI or the REST API.
type ReleaseOps interface {
	QueryRelease(ctx context.Context, repo github.Repo, tag string) github.ReleaseQuery
	CreateRelease(ctx context.Context, repo github.Repo, tag, title, notes string, prerelease bool) error
	UploadReleaseAssets(ctx context.Context, repo github.Repo, tag string, files []string, replace bool) error
}

// githubReleaseOps is the ReleaseOps the published run uses.
type githubReleaseOps struct {
	run execx.Runner
}

func (o githubReleaseOps) QueryRelease(ctx context.Context, repo github.Repo, tag string) github.ReleaseQuery {
	return github.QueryRelease(ctx, o.run, repo, tag)
}

func (o githubReleaseOps) CreateRelease(ctx context.Context, repo github.Repo, tag, title, notes string, prerelease bool) error {
	return github.CreateRelease(ctx, o.run, repo, tag, title, notes, prerelease)
}

func (o githubReleaseOps) UploadReleaseAssets(ctx context.Context, repo github.Repo, tag string, files []string, replace bool) error {
	return github.UploadReleaseAssets(ctx, o.run, repo, tag, files, replace)
}

// releaseOps returns the injected release surface or the real one.
func (d *Deps) releaseOps() ReleaseOps {
	if d.Releases != nil {
		return d.Releases
	}
	return githubReleaseOps{run: d.Run}
}

// repairAction is one verified release step. Everything in it was checked before
// the first write, so applying it can only fail on a transport error.
type repairAction struct {
	release    ReleasePlan
	create     bool
	title      string
	notes      string
	prerelease bool
	// upload lists the recorded attachments the remote does not have.
	upload []string
	// unverified lists attachments the remote reports without a digest. They are
	// never replaced, and their presence is not counted as verified.
	unverified []string
}

// Repair finishes a recorded publish run whose only missing step is the GitHub
// Release. It returns the plan reconstructed from the marker and whether the
// recorded release was the subject of this resume: true means the caller must
// not plan a new release, so the records of the next version stay untouched.
//
// A resume never plans a release when the committed marker describes a release
// that already has its tag: such a plan could only bump the next version and
// consume the records that belong to it. Only a marker release that still lacks
// its tag falls through to the normal path, because continuing it cannot bump
// anything.
func Repair(ctx context.Context, d *Deps) (*Plan, bool, error) {
	marker, err := state.LoadMarker(d.Root)
	if err != nil {
		return nil, false, err
	}
	if marker == nil || len(marker.Releases) == 0 {
		// Nothing is recorded, so this is a first release and the normal path owns
		// the decision.
		return nil, false, nil
	}
	open, err := markerReleaseOpen(ctx, d, marker)
	if err != nil {
		return nil, false, err
	}
	subject := markerSubject(marker)
	// stop ends the resume without writing anything. An open marker release falls
	// through, because the normal path can only continue it; a closed one stops
	// here and reports why, with the message already printed when it is empty.
	stop := func(message string) (*Plan, bool, error) {
		if open {
			return nil, false, nil
		}
		if message != "" {
			d.UI.Info("%s", message)
		}
		return repairPlan(d, marker), true, nil
	}

	run, err := state.LatestRun(d.Root)
	if err != nil {
		return nil, false, err
	}
	if run != nil && publishStepsUnfinished(run) {
		// The recorded release still owes a publish step - an upload or a tag
		// push. A repair only answers for a release whose publish phase finished,
		// so the normal resume continues it and never plans a new version.
		return nil, false, nil
	}
	if run == nil {
		// Without a local record the ledger is the only evidence. When it is gone
		// too, a release that still owes an upload is unknown rather than
		// repairable: the publish phase names the package and says how to check it.
		// A release whose packages publish by tag needs nothing here, so stopping
		// with a reason is enough.
		entry, entryErr := LedgerEntryFor(ctx, d, true)
		if entryErr != nil || entry == nil {
			if uploadsNeeded(d, marker) {
				return nil, false, nil
			}
		}
	}
	if run == nil || !sameRecordedRelease(marker, run) || !recordedTagsCreated(run) {
		// There is no evidence of an interrupted publish, and the recorded release
		// is already out: do not plan a new one.
		return stop(i18n.Tf("resume.repair.stopped", subject))
	}
	// A resume that already finished this release stops here instead of looking at
	// the remote again.
	if step, ok := run.Steps[stepGitHubReleaseRepair]; ok && step.Status == state.StatusDone {
		if waiting := waitingRecords(d, marker); waiting > 0 {
			return stop(i18n.Tf("resume.repair.pending", subject, waiting))
		}
		return stop(i18n.Tf("resume.repair.settled", subject))
	}
	// A finished release step has nothing to recover; leave the run alone so a
	// healthy repository resumes exactly as before.
	if step, ok := run.Steps["github-release"]; ok && step.Status == state.StatusDone {
		return stop(i18n.Tf("resume.repair.stopped", subject))
	}
	if !d.Cfg.Release.GitHubRelease {
		d.UI.Info("%s", i18n.T("resume.repair.config_disabled"))
		return stop("")
	}
	if d.Options.NoGitHubRelease {
		d.UI.Info("%s", i18n.T("resume.repair.flag_disabled"))
		return stop("")
	}
	if step, ok := run.Steps["github-release"]; ok && step.Status == state.StatusSkipped {
		// The recorded run skipped the release on purpose; keep that decision
		// instead of publishing what the operator left out.
		d.UI.Info("%s", i18n.T("resume.repair.original_disabled"))
		return stop("")
	}
	if !recordedReleaseFailed(run) {
		// The run never tried to create the release, so there is no evidence that
		// the release is the only missing step. Do not guess from a partially
		// written run record.
		return stop(i18n.Tf("resume.repair.stopped", subject))
	}
	if !uploadsFinished(marker, run) {
		d.UI.Warn("%s", i18n.T("resume.repair.uploads_pending"))
		return stop("")
	}
	repo, err := repairRepo(ctx, d, marker)
	if err != nil {
		return nil, true, err
	}
	if !repo.Valid() {
		return nil, true, errors.New(i18n.T("resume.repair.no_repo"))
	}
	e := &executor{d: d, plan: repairPlan(d, marker), run: run}
	ops := d.releaseOps()
	remote := d.Cfg.Git.Remote
	if remote == "" {
		remote = "origin"
	}

	// First pass: verify every release and every expected attachment. Nothing is
	// written until all of them are known to be repairable.
	actions := make([]repairAction, 0, len(e.plan.Releases))
	for _, release := range e.plan.Releases {
		action, err := e.planRelease(ctx, ops, repo, remote, release)
		if err != nil {
			return nil, true, err
		}
		if action == nil {
			// The remote tag is not there yet, so the recorded release is not the
			// only missing step: plan again instead of writing part of it.
			return stop("")
		}
		actions = append(actions, *action)
	}

	// Second pass: write.
	for _, action := range actions {
		if err := e.applyRelease(ctx, ops, repo, action); err != nil {
			return nil, true, err
		}
	}
	if err := e.recordRepair(); err != nil {
		return nil, true, err
	}
	return e.plan, true, nil
}

// planRelease verifies one recorded release without writing anything. A nil
// action means the remote tag is not there yet, so the release is not the
// subject of this resume.
func (e *executor) planRelease(ctx context.Context, ops ReleaseOps, repo github.Repo, remote string, release ReleasePlan) (*repairAction, error) {
	remoteCommit, found, err := e.d.Git.RemoteTagCommit(ctx, remote, release.Tag)
	if err != nil {
		return nil, errors.New(i18n.Tf("resume.repair.tag_unverifiable", release.Tag, remote, github.CauseLine(err)))
	}
	if !found {
		e.d.UI.Warn("%s", i18n.Tf("resume.repair.tag_missing", remote, release.Tag))
		return nil, nil
	}
	localCommit, err := e.d.Git.TagCommit(ctx, release.Tag)
	if err != nil {
		return nil, err
	}
	if !sameCommit(localCommit, remoteCommit) {
		return nil, errors.New(i18n.Tf("resume.repair.tag_mismatch", release.Tag, shortSHA(remoteCommit), shortSHA(localCommit)))
	}
	query := ops.QueryRelease(ctx, repo, release.Tag)
	switch query.Status {
	case github.ReleaseFound:
	case github.ReleaseMissing:
	default:
		// Unauthorized and unverifiable both mean the remote state is unknown, so
		// nothing may be written.
		return nil, errors.New(query.Cause)
	}
	upload, unverified, err := e.missingAssets(query, release)
	if err != nil {
		return nil, err
	}
	action := &repairAction{release: release, upload: upload, unverified: unverified}
	if query.Status == github.ReleaseMissing {
		action.create = true
		action.title, action.notes, action.prerelease = e.releasePayload(release)
	} else {
		e.warnReleaseAttributes(query, release)
	}
	return action, nil
}

// applyRelease carries out one verified action.
func (e *executor) applyRelease(ctx context.Context, ops ReleaseOps, repo github.Repo, action repairAction) error {
	release := action.release
	created := false
	if action.create {
		e.d.UI.Info("%s", i18n.Tf("resume.repair.start", release.Tag))
		if err := ops.CreateRelease(ctx, repo, release.Tag, action.title, action.notes, action.prerelease); err != nil {
			if !errors.Is(err, github.ErrExists) {
				return err
			}
			// A concurrent run created the release first.
			e.d.UI.Info("%s", i18n.Tf("release.msg.release_exists", release.Tag))
		} else {
			created = true
			e.d.UI.Success("%s", i18n.Tf("resume.repair.created", release.Tag))
		}
	}
	if len(action.upload) > 0 {
		if err := ops.UploadReleaseAssets(ctx, repo, release.Tag, action.upload, false); err != nil {
			return err
		}
		e.d.UI.Success("%s", i18n.Tf("release.msg.assets_uploaded", len(action.upload), release.Tag))
	}
	if created || len(action.upload) > 0 {
		return nil
	}
	// Nothing was written. Report the state without claiming a comparison that
	// did not happen.
	switch {
	case len(action.unverified) > 0:
		e.d.UI.Warn("%s", i18n.Tf("resume.repair.unverified", release.Tag, len(action.unverified)))
	case e.comparedAssets():
		// The remote already holds everything the recorded run planned, so the
		// recorded release is still the subject of this resume: finishing it must
		// not plan the next version.
		e.d.UI.Info("%s", i18n.Tf("resume.repair.complete", release.Tag))
	default:
		e.d.UI.Info("%s", i18n.Tf("resume.repair.present", release.Tag))
	}
	return nil
}

// uploadsNeeded reports whether the marker names a package whose publication is
// an upload rather than a tag push, so its outcome has to be known.
func uploadsNeeded(d *Deps, marker *state.Marker) bool {
	for _, pkg := range markerPackages(marker.Packages) {
		if !pkg.Publish {
			continue
		}
		if !registry.PublishesByTag(pkg.Resolver) {
			return true
		}
	}
	return false
}

// publishStepsUnfinished reports whether the recorded run still owes a publish
// step. A repair cannot finish those - it does not upload packages or push tags
// - so such a release belongs to the publish phase of the resume.
func publishStepsUnfinished(run *state.RunRecord) bool {
	for _, step := range []string{"publish", "tag", "push"} {
		recorded, ok := run.Steps[step]
		if !ok {
			continue
		}
		switch recorded.Status {
		case state.StatusDone, state.StatusSkipped:
		default:
			return true
		}
	}
	return false
}

// recordRepair stores the outcome on the same run record. The run id does not
// change, so latest.json keeps pointing at the record that still carries the
// evidence of the interrupted run. A failure is an error: the remote write
// already happened, and leaving the record saying "failed" would hide it.
func (e *executor) recordRepair() error {
	e.run.Mark("github-release", state.StatusDone, nil)
	e.run.Mark(stepGitHubReleaseRepair, state.StatusDone, nil)
	if err := state.SaveRun(e.d.Root, e.run); err != nil {
		return errors.New(i18n.Tf("resume.repair.save_failed", err))
	}
	return nil
}

// comparedAssets reports whether the run compared its attachments with the
// remote. Assets belong to a single-release run; the publish path uploads them
// under the same rule.
func (e *executor) comparedAssets() bool {
	return len(e.plan.Releases) == 1 && len(e.run.Assets) > 0
}

// missingAssets resolves the attachments of the recorded run and returns the
// local paths that still have to be uploaded, plus the ones the remote reports
// without a digest. An attachment that is present with another digest is a
// conflict, and one that is gone locally cannot be proven to belong to this
// release: both stop the repair before it writes anything.
func (e *executor) missingAssets(query github.ReleaseQuery, release ReleasePlan) ([]string, []string, error) {
	if !e.comparedAssets() {
		return nil, nil, nil
	}
	var missing, unverified []string
	for _, path := range e.run.Assets {
		name := filepath.Base(path)
		digest, err := fileDigest(path)
		if err != nil {
			// TODO(merge): rebuild the attachment inside an isolated snapshot of the
			// release source commit (PR-05 release identity) instead of asking the
			// operator to reproduce it by hand.
			return nil, nil, errors.New(i18n.Tf("resume.repair.needs_rebuild", name, release.Tag))
		}
		if query.Status == github.ReleaseFound {
			if asset, ok := query.Asset(name); ok {
				if asset.Digest == "" {
					// An existing asset is not missing, and without a digest the
					// remote answer cannot prove which bytes it holds. It is never
					// replaced, and it is never reported as verified either.
					unverified = append(unverified, name)
					continue
				}
				if !sameDigest(asset.Digest, digest) {
					return nil, nil, errors.New(i18n.Tf("resume.repair.conflict", name, release.Tag, asset.Digest, digest))
				}
				continue
			}
		}
		missing = append(missing, path)
	}
	return missing, unverified, nil
}

// warnReleaseAttributes reports a release whose prerelease flag no longer
// matches the recorded decision. A repair never rewrites an existing release, so
// this warning is the only visible evidence.
func (e *executor) warnReleaseAttributes(query github.ReleaseQuery, release ReleasePlan) {
	if query.Release.Prerelease != e.prereleaseOf(release) {
		e.d.UI.Warn("%s", i18n.Tf("resume.repair.prerelease_mismatch", release.Tag))
	}
}

// prereleaseOf reports whether the recorded release is a prerelease.
func (e *executor) prereleaseOf(release ReleasePlan) bool {
	return e.plan.Channel != "" || release.Prerelease != ""
}

// repairPlan rebuilds the plan the recorded publish run executed, so the notes
// and the prerelease flag come from the same rules the original run used.
func repairPlan(d *Deps, marker *state.Marker) *Plan {
	plan := &Plan{
		SchemaVersion: 2,
		Phase:         PhasePublish,
		Channel:       marker.Channel,
		Fingerprint:   marker.Fingerprint,
		Releases:      markerReleasePlans(d, marker, nil),
		Packages:      markerPackages(marker.Packages),
		Changes:       markerChanges(marker.Changes),
		Version:       VersionInfo{Next: marker.Version},
		Marker:        marker,
	}
	if marker.Repo.Valid() {
		plan.Repo = github.Repo{Host: marker.Repo.Host, Owner: marker.Repo.Owner, Name: marker.Repo.Name}
	}
	if len(plan.Releases) == 1 {
		plan.Tag = plan.Releases[0].Tag
		plan.PreviousTag = plan.Releases[0].PrevTag
		if plan.Version.Next == "" {
			plan.Version.Next = plan.Releases[0].Version
		}
	}
	return plan
}

// repairRepo resolves the repository the recorded release belongs to. The marker
// records it, so a moved remote cannot point the repair at another repository.
func repairRepo(ctx context.Context, d *Deps, marker *state.Marker) (github.Repo, error) {
	if marker.Repo.Valid() {
		return github.Repo{Host: marker.Repo.Host, Owner: marker.Repo.Owner, Name: marker.Repo.Name}, nil
	}
	repo, ok := github.Detect(ctx, d.Run, d.Root, d.Cfg.Git.Remote)
	if !ok {
		return github.Repo{}, nil
	}
	return repo, nil
}

// sameRecordedRelease reports whether the recorded run executed the release the
// marker describes. A missing fingerprint on either side comes from a marker or
// a run written by an older qegg and is not evidence of a mismatch.
func sameRecordedRelease(marker *state.Marker, run *state.RunRecord) bool {
	if marker.Fingerprint == "" || run.Fingerprint == "" {
		return true
	}
	return marker.Fingerprint == run.Fingerprint
}

// recordedReleaseFailed reports whether the recorded run actually tried to
// create the GitHub Release and failed. The step has to exist: a run that stopped
// earlier, or a hand-edited record, is no evidence that only the release is
// missing.
func recordedReleaseFailed(run *state.RunRecord) bool {
	step, ok := run.Steps["github-release"]
	if !ok {
		return false
	}
	return step.Status == state.StatusFailed || step.Status == state.StatusRunning
}

// markerSubject names the release the marker describes for a message.
func markerSubject(marker *state.Marker) string {
	if len(marker.Releases) == 1 && marker.Releases[0].Tag != "" {
		return marker.Releases[0].Tag
	}
	return marker.Version
}

// markerReleaseOpen reports whether any release the marker describes still lacks
// its local tag. Only such a release can be continued by the normal path without
// planning a new version.
func markerReleaseOpen(ctx context.Context, d *Deps, marker *state.Marker) (bool, error) {
	for _, release := range marker.Releases {
		exists, err := d.Git.TagExists(ctx, release.Tag)
		if err != nil {
			return false, err
		}
		if !exists {
			return true, nil
		}
	}
	return false, nil
}

// waitingRecords counts the change records the marker has not consumed yet, which
// are the ones the next version would release.
func waitingRecords(d *Deps, marker *state.Marker) int {
	records, err := changes.LoadDir(changesDir(d))
	if err != nil {
		return 0
	}
	consumed := make(map[string]bool, len(marker.Changes))
	for _, change := range marker.Changes {
		consumed[change.File] = true
	}
	waiting := 0
	for _, record := range changes.Consumed(records) {
		if !consumed[fsx.Rel(d.Root, record.Path)] {
			waiting++
		}
	}
	return waiting
}

// recordedTagsCreated reports whether the recorded run finished creating the
// release tags. Without that the GitHub Release is not the only missing step, so
// the normal release path has to plan the run again.
func recordedTagsCreated(run *state.RunRecord) bool {
	step, ok := run.Steps["tag"]
	if !ok {
		return false
	}
	return step.Status == state.StatusDone || step.Status == state.StatusSkipped
}

// uploadsFinished reports whether the recorded run completed or explicitly
// skipped every package upload. Only then may a repair create a release: a
// release published over a half-finished upload would look complete.
func uploadsFinished(marker *state.Marker, run *state.RunRecord) bool {
	if step, ok := run.Steps["publish"]; ok && step.Status == state.StatusSkipped {
		return true
	}
	for _, pkg := range marker.Packages {
		if !pkg.Publish {
			continue
		}
		published, ok := run.Publish[pkg.Name]
		if !ok {
			return false
		}
		// The recorded outcome of an upload the registry already had. The value
		// matches the package state publishPackages writes; PR-06 owns that
		// function and can switch it to state.StatusSkippedExists.
		if published.Status != state.StatusDone && published.Status != state.StatusSkippedExists {
			return false
		}
	}
	return true
}

// sameCommit compares two commit ids, tolerating an abbreviated one.
func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// fileDigest returns the sha256 digest of one file in the spelling the GitHub
// API uses.
func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

// sameDigest compares two digests, ignoring the algorithm prefix and case.
func sameDigest(a, b string) bool {
	normalized := normalizeDigest(a)
	return normalized != "" && normalized == normalizeDigest(b)
}

func normalizeDigest(digest string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(digest)), "sha256:")
}
