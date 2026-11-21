package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// The scenarios below resume a half finished release in process and count every
// external write. Each one asserts the work the resume had to do before it
// asserts that the run after it writes nothing.

// TestResumePublishesOnlyTheMissingPackage is the partial upload scenario: A is
// recorded, B's tool never started, and a new runner repeats neither of them.
func TestResumePublishesOnlyTheMissingPackage(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	beta := pkgSpec{name: "beta", dir: "beta", prev: "2.0.0", version: "2.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha, beta)
	// beta's publish command is not installed: the request never leaves.
	f.runner.failures[f.dir(beta)] = &fakeFailure{notFound: true}

	if err := f.resume(release.Options{}); err == nil {
		t.Fatal("the run must stop when beta cannot be published")
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("alpha publish count = %d, want 1", got)
	}
	if got := f.runner.publishes(f.dir(beta)); got != 1 {
		t.Fatalf("beta attempt count = %d, want 1", got)
	}
	if record := f.record("alpha"); record.Outcome != state.OutcomeUploaded {
		t.Fatalf("alpha outcome = %q, want uploaded", record.Outcome)
	}
	if record := f.record("beta"); record.Outcome != state.OutcomeNotStarted {
		t.Fatalf("beta outcome = %q, want not-started", record.Outcome)
	}
	if tags := f.remoteTags(); strings.Contains(tags, f.tag(alpha)) {
		t.Fatalf("a failed publish must not push tags: %s", tags)
	}

	// A new CI runner: it has no local state and reads the record from origin.
	clone := f.clone()
	if err := f.resumeAt(clone, release.Options{}); err != nil {
		t.Fatalf("resume on the new runner: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("alpha was uploaded again from the new runner: %d", got)
	}
	if got := f.runner.publishes(filepath.Join(clone, "beta")); got != 1 {
		t.Fatalf("beta publish count on the new runner = %d, want 1", got)
	}
	tags := f.remoteTags()
	for _, spec := range []pkgSpec{alpha, beta} {
		if !strings.Contains(tags, f.tag(spec)) {
			t.Fatalf("remote tags %q are missing %s", tags, f.tag(spec))
		}
	}
	if got := f.runner.releaseCount(); got != 2 {
		t.Fatalf("GitHub Releases created = %d, want 2", got)
	}

	// Running the same resume again must perform no external write at all.
	before := f.remoteTags()
	if err := f.resumeAt(clone, release.Options{}); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("alpha published a third time: %d", got)
	}
	if got := f.runner.publishes(filepath.Join(clone, "beta")); got != 1 {
		t.Fatalf("beta published a second time: %d", got)
	}
	if got := f.runner.releaseCount(); got != 2 {
		t.Fatalf("another GitHub Release was created: %d", got)
	}
	if after := f.remoteTags(); after != before {
		t.Fatalf("the resume moved remote tags:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestResumeSkipsAnUploadWhoseResultWasLost is the second half of the same
// scenario: the server accepted the upload and only the response was lost.
func TestResumeSkipsAnUploadWhoseResultWasLost(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	f.runner.failures[f.dir(alpha)] = &fakeFailure{}

	if err := f.resume(release.Options{}); err == nil {
		t.Fatal("a lost publish result must stop the release")
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("publish attempts = %d, want 1", got)
	}
	if record := f.record("alpha"); record.Outcome != state.OutcomeUnknown {
		t.Fatalf("outcome = %q, want unknown", record.Outcome)
	}
	if tags := f.remoteTags(); strings.Contains(tags, f.tag(alpha)) {
		t.Fatalf("no tag may be pushed after a failed publish: %s", tags)
	}

	// The target registry did receive the version.
	f.registry.set("alpha", "1.1.0", http.StatusOK)
	clone := f.clone()
	if err := f.resumeAt(clone, release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := f.runner.publishes(filepath.Join(clone, "alpha")); got != 0 {
		t.Fatalf("the confirmed version was uploaded again: %d", got)
	}
	if record := f.record("alpha"); record.Outcome != state.OutcomeUploaded {
		t.Fatalf("outcome = %q, want uploaded", record.Outcome)
	}
	if !strings.Contains(f.remoteTags(), f.tag(alpha)) {
		t.Fatal("the confirmed release must still be tagged and pushed")
	}
	if got := f.runner.releaseCount(); got != 1 {
		t.Fatalf("GitHub Releases created = %d, want 1", got)
	}
}

// TestResumeKeepsTheOriginalSkipDecision covers --no-publish: the first run
// records the decision and the resume repeats it without the flag.
func TestResumeKeepsTheOriginalSkipDecision(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)

	if err := f.resume(release.Options{NoPublish: true}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 0 {
		t.Fatalf("--no-publish uploaded %d package(s)", got)
	}
	// The skipped run still tags, pushes and releases.
	if !strings.Contains(f.remoteTags(), f.tag(alpha)) {
		t.Fatal("a skipped publish must still push its tag")
	}
	if got := f.runner.releaseCount(); got != 1 {
		t.Fatalf("GitHub Releases created = %d, want 1", got)
	}
	record := f.record("alpha")
	if record.Outcome != state.OutcomeSkipped || record.Reason != state.ReasonNoPublish {
		t.Fatalf("record = %+v, want skipped/%s", record, state.ReasonNoPublish)
	}

	// The resume omits the flag on purpose.
	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 0 {
		t.Fatalf("the stored --no-publish was ignored: %d upload(s)", got)
	}
}

// TestResumePublishesOnlyTheEnabledPackage covers publish = false on one package.
func TestResumePublishesOnlyTheEnabledPackage(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	beta := pkgSpec{name: "beta", dir: "beta", prev: "2.0.0", version: "2.1.0", resolver: "npm", publish: false}
	f := newPublishFixture(t, config.ModeCommit, alpha, beta)

	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("alpha publish count = %d, want 1", got)
	}
	if got := f.runner.publishes(f.dir(beta)); got != 0 {
		t.Fatalf("a package with publish = false was uploaded %d time(s)", got)
	}
	record := f.record("beta")
	if record.Outcome != state.OutcomeSkipped || record.Reason != state.ReasonPublishFalse {
		t.Fatalf("beta record = %+v, want skipped/%s", record, state.ReasonPublishFalse)
	}
	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("alpha was uploaded again: %d", got)
	}
}

// TestResumePushesOnlyTheMissingTag covers a local tag whose push failed.
func TestResumePushesOnlyTheMissingTag(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	allowPushes := f.rejectTagPushes()

	if err := f.resume(release.Options{}); err == nil {
		t.Fatal("a refused tag push must stop the release")
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("publish count = %d, want 1", got)
	}
	if tags := f.remoteTags(); strings.Contains(tags, f.tag(alpha)) {
		t.Fatalf("the tag should not be on the remote yet: %s", tags)
	}
	local := gitText(t, f.root, "rev-parse", f.tag(alpha))
	if local == "" {
		t.Fatal("the local tag is missing")
	}

	allowPushes()
	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("the upload repeated after the tag push failed: %d", got)
	}
	if got := gitText(t, f.root, "rev-parse", f.tag(alpha)); got != local {
		t.Fatalf("the local tag was recreated: %s -> %s", local, got)
	}
	if !strings.Contains(f.remoteTags(), f.tag(alpha)) {
		t.Fatal("the missing tag was not pushed")
	}

	// A remote tag that points at another commit stops the release.
	f.pointRemoteTag(f.tag(alpha), "HEAD~1")
	before := f.remoteTags()
	if err := f.resume(release.Options{}); err == nil {
		t.Fatal("a conflicting remote tag must stop the resume")
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("publish count after the conflict = %d, want 1", got)
	}
	if got := f.runner.releaseCount(); got != 1 {
		t.Fatalf("a conflicting tag created a GitHub Release: %d", got)
	}
	if after := f.remoteTags(); after != before {
		t.Fatalf("the conflicting resume moved the remote tag:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestResumePublishesJavaOnlyWithARecord covers the ecosystem without a version
// query: the recorded attempt is what makes a deploy legal.
func TestResumePublishesJavaOnlyWithARecord(t *testing.T) {
	lib := pkgSpec{name: "lib", dir: "lib", prev: "0.9.0", version: "1.0.0", resolver: "java", publish: true}
	f := newPublishFixture(t, config.ModeCommit, lib)

	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := f.runner.publishes(f.dir(lib)); got != 1 {
		t.Fatalf("deploy count = %d, want 1", got)
	}
	if record := f.record("lib"); record.Outcome != state.OutcomeUploaded || record.Key.Target != "maven" {
		t.Fatalf("record = %+v, want uploaded/maven", record)
	}
	if !strings.Contains(f.remoteTags(), f.tag(lib)) {
		t.Fatal("the release tag was not pushed")
	}

	// A runner that never ran this release, and a record that is gone: nothing may
	// be deployed, because the release was recorded when it was planned.
	clone := f.clone()
	git(t, f.root, "push", f.remote, "--delete", release.LedgerRefPrefix+f.ledgerTag())
	if err := f.resumeAt(clone, release.Options{}); err == nil {
		t.Fatal("a release whose record is lost must stop for an ecosystem that cannot query")
	} else if !strings.Contains(err.Error(), "lib") {
		t.Fatalf("the error must name the package: %v", err)
	}
	if got := f.runner.publishes(filepath.Join(clone, "lib")); got != 0 {
		t.Fatalf("a release with a lost record deployed %d time(s)", got)
	}
	// A record this run had to create is read as "no earlier attempt", so the same
	// release without a tag is allowed to deploy again (see freshRelease in
	// ledger_session.go and the trade-off documented in docs/releasing.md). The
	// tag is therefore what protects the resume above, which is why the release
	// tag is always created after the uploads and never removed automatically.
}

// TestJavaResumeStopsWhenTheDeployResultIsUnknown is the third partial upload
// case: the deploy ran, the answer was lost and the ecosystem cannot ask.
func TestJavaResumeStopsWhenTheDeployResultIsUnknown(t *testing.T) {
	lib := pkgSpec{name: "lib", dir: "lib", prev: "0.9.0", version: "1.0.0", resolver: "java", publish: true}
	f := newPublishFixture(t, config.ModeCommit, lib)
	f.runner.failures[f.dir(lib)] = &fakeFailure{}

	if err := f.resume(release.Options{}); err == nil {
		t.Fatal("the first run must stop")
	}
	if got := f.runner.publishes(f.dir(lib)); got != 1 {
		t.Fatalf("deploy count = %d, want 1", got)
	}
	if record := f.record("lib"); record.Outcome != state.OutcomeUnknown {
		t.Fatalf("outcome = %q, want unknown", record.Outcome)
	}
	if tags := f.remoteTags(); strings.Contains(tags, f.tag(lib)) {
		t.Fatalf("no tag may be pushed while the deploy result is unknown: %s", tags)
	}

	if err := f.resume(release.Options{}); err == nil {
		t.Fatal("the resume must refuse to deploy again")
	} else if !strings.Contains(err.Error(), "lib") {
		t.Fatalf("the error must name the package: %v", err)
	}
	if got := f.runner.publishes(f.dir(lib)); got != 1 {
		t.Fatalf("the deploy repeated after an unknown result: %d", got)
	}
}

// TestConcurrentResumesPublishOnce runs two resumes of the same release at the
// same time: the ledger keeps the second one from starting a second upload.
func TestConcurrentResumesPublishOnce(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	blocker := make(chan struct{})
	entered := make(chan struct{}, 1)
	f.runner.blocked[f.dir(alpha)] = blocker
	f.runner.entered[f.dir(alpha)] = entered

	var wg sync.WaitGroup
	var firstErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstErr = f.resume(release.Options{})
	}()
	<-entered // the first run recorded "started" and is inside the request
	secondErr := f.resume(release.Options{})
	close(blocker)
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("the owning run failed: %v", firstErr)
	}
	if secondErr == nil {
		t.Fatal("the second resume must stop while another run owns the release")
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("uploads = %d, want 1", got)
	}
}

// TestResumeRecordsTheAttemptBeforeTheRequest reads the ledger from inside the
// upload command: the "started" record must already be there, otherwise a second
// run could not tell an attempt from a package that was never touched.
func TestResumeRecordsTheAttemptBeforeTheRequest(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	var seen []state.PackageOutcome
	f.runner.onPublish = func(dir string) {
		if dir != f.dir(alpha) {
			return
		}
		seen = append(seen, f.record("alpha").Outcome)
	}

	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("the publish command ran %d time(s), want 1", len(seen))
	}
	if seen[0] != state.OutcomeStarted {
		t.Fatalf("the ledger said %q while the request was in flight, want started", seen[0])
	}
	if record := f.record("alpha"); record.Outcome != state.OutcomeUploaded {
		t.Fatalf("outcome after the run = %q, want uploaded", record.Outcome)
	}
}

// TestLegacyReleaseStopsWithoutARecord is the release an older qegg planned: the
// marker proves nothing about the uploads and the ecosystem cannot query, so no
// deploy may happen — not even a first one.
func TestLegacyReleaseStopsWithoutARecord(t *testing.T) {
	lib := pkgSpec{name: "lib", dir: "lib", prev: "0.9.0", version: "1.0.0", resolver: "java", publish: true}
	f := newLegacyPublishFixture(t, lib)
	if revision := f.ledgerRevision(); revision != "" {
		t.Fatalf("the legacy fixture must not have a ledger ref: %q", revision)
	}

	err := f.resume(release.Options{})
	if err == nil {
		t.Fatal("a legacy release without a record must not deploy")
	}
	if !strings.Contains(err.Error(), "lib") {
		t.Fatalf("the error must name the package: %v", err)
	}
	if got := f.runner.publishes(f.dir(lib)); got != 0 {
		t.Fatalf("a release without a record deployed %d time(s)", got)
	}
	if tags := f.remoteTags(); strings.Contains(tags, f.tag(lib)) {
		t.Fatalf("a refused release must not push its tag: %s", tags)
	}
}

// TestResumeStopsWhenTheRegistryForbidsAccess covers scenario 1's 401: the query
// answer is not "missing", so nothing may be written.
func TestResumeStopsWhenTheRegistryForbidsAccess(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	f.registry.set("alpha", "1.1.0", http.StatusUnauthorized)

	err := f.resume(release.Options{})
	if err == nil {
		t.Fatal("a refused registry query must stop the release")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("the error must name the package: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 0 {
		t.Fatalf("uploads after a refused query: %d", got)
	}
	if tags := f.remoteTags(); strings.Contains(tags, f.tag(alpha)) {
		t.Fatalf("a refused query must not push the tag: %s", tags)
	}
	if got := f.runner.releaseCount(); got != 0 {
		t.Fatalf("a refused query created %d GitHub Release(s)", got)
	}
	if record := f.record("alpha"); record.Outcome != state.OutcomeNotStarted {
		t.Fatalf("outcome = %q, want not-started", record.Outcome)
	}
}

// TestResumeDoesNotUploadTheAttachmentsAgain is the acceptance rule for the
// attachments: the second resume must not rebuild or re-upload them.
func TestResumeDoesNotUploadTheAttachmentsAgain(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	assets := t.TempDir()
	for _, name := range []string{"alpha_1.1.0_linux_amd64.tar.gz", "checksums.txt"} {
		if err := os.WriteFile(filepath.Join(assets, name), []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	options := release.Options{AssetsDir: assets}

	if err := f.resume(options); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := f.runner.uploadCount(); got != 1 {
		t.Fatalf("attachment uploads = %d, want 1", got)
	}
	if err := f.resume(options); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := f.runner.uploadCount(); got != 1 {
		t.Fatalf("the attachments were uploaded again: %d upload(s)", got)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 1 {
		t.Fatalf("the package was published again: %d", got)
	}
}

// TestResumeKeepsTheSkipDecisionOfAQuietRun is the TASKS line 57 requirement: a
// run that was allowed no external write still records its decision, and a later
// flag-less resume repeats it instead of publishing.
func TestResumeKeepsTheSkipDecisionOfAQuietRun(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)
	f.cfg.Release.GitHubRelease = false

	// A planning phase that was allowed no external write leaves no remote record.
	git(t, f.root, "push", f.remote, "--delete", release.LedgerRefPrefix+f.ledgerTag())

	quiet := release.Options{NoPublish: true, NoPush: true, NoGitHubRelease: true}
	if err := f.resume(quiet); err != nil {
		t.Fatalf("quiet run: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 0 {
		t.Fatalf("the quiet run uploaded %d package(s)", got)
	}
	if revision := f.ledgerRevision(); revision != "" {
		t.Fatalf("a quiet run must not write to the remote: %q", revision)
	}

	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := f.runner.publishes(f.dir(alpha)); got != 0 {
		t.Fatalf("the skip decision of the quiet run was lost: %d upload(s)", got)
	}
	if record := f.record("alpha"); record.Outcome != state.OutcomeSkipped || record.Reason != state.ReasonNoPublish {
		t.Fatalf("record = %+v, want skipped/%s", record, state.ReasonNoPublish)
	}
}

// TestStatusShowsTheRecordedSkip is scenario 3's last sentence: qegg status has
// to report the decision of the original run, not "unknown".
func TestStatusShowsTheRecordedSkip(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	f := newPublishFixture(t, config.ModeCommit, alpha)

	if err := f.resume(release.Options{NoPublish: true}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	for _, offline := range []bool{true, false} {
		report := f.statusReport(offline)
		if report.Publish == nil {
			t.Fatalf("status (offline=%v) has no publish record", offline)
		}
		if len(report.Packages) != 1 {
			t.Fatalf("status lists %d packages, want 1", len(report.Packages))
		}
		pkg := report.Packages[0]
		if pkg.Outcome != string(state.OutcomeSkipped) || pkg.Reason != state.ReasonNoPublish {
			t.Fatalf("status (offline=%v) reports %q/%q, want skipped/%s", offline, pkg.Outcome, pkg.Reason, state.ReasonNoPublish)
		}
	}
}

// TestIdempotentResumeKeepsTheLedgerRevision pins that a resume changes nothing
// when nothing changed, including the revision of the ledger ref. The skipped
// package is the interesting one: its record is rewritten on every resume unless
// the write is suppressed.
func TestIdempotentResumeKeepsTheLedgerRevision(t *testing.T) {
	alpha := pkgSpec{name: "alpha", dir: "alpha", prev: "1.0.0", version: "1.1.0", resolver: "npm", publish: true}
	beta := pkgSpec{name: "beta", dir: "beta", prev: "2.0.0", version: "2.1.0", resolver: "npm", publish: false}
	f := newPublishFixture(t, config.ModeCommit, alpha, beta)

	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	before := f.ledgerRevision()
	if before == "" {
		t.Fatal("the release did not record anything")
	}
	if err := f.resume(release.Options{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if after := f.ledgerRevision(); after != before {
		t.Fatalf("an idempotent resume rewrote the ledger:\nbefore: %s\nafter:  %s", before, after)
	}
}
