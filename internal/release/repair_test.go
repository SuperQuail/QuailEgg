package release

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// fakeReleaseOps models one GitHub remote and counts every external write, so a
// test can assert that a resumed run writes exactly once and never again.
type fakeReleaseOps struct {
	releases   map[string]*github.ReleaseSnapshot
	queryFault *github.ReleaseQuery
	queries    []string
	creates    []string
	uploads    [][]string
	replaces   []bool
	titles     map[string]string
	notes      map[string]string
	prerelease map[string]bool
}

func newFakeReleaseOps() *fakeReleaseOps {
	return &fakeReleaseOps{
		releases:   map[string]*github.ReleaseSnapshot{},
		titles:     map[string]string{},
		notes:      map[string]string{},
		prerelease: map[string]bool{},
	}
}

func (f *fakeReleaseOps) QueryRelease(_ context.Context, _ github.Repo, tag string) github.ReleaseQuery {
	f.queries = append(f.queries, tag)
	if f.queryFault != nil {
		return *f.queryFault
	}
	if release, ok := f.releases[tag]; ok {
		return github.ReleaseQuery{Status: github.ReleaseFound, Release: *release}
	}
	return github.ReleaseQuery{Status: github.ReleaseMissing, Cause: "no release " + tag}
}

func (f *fakeReleaseOps) CreateRelease(_ context.Context, _ github.Repo, tag, title, notes string, prerelease bool) error {
	if _, ok := f.releases[tag]; ok {
		return github.ErrExists
	}
	f.releases[tag] = &github.ReleaseSnapshot{Tag: tag, Name: title, Notes: notes, Prerelease: prerelease}
	f.creates = append(f.creates, tag)
	f.titles[tag] = title
	f.notes[tag] = notes
	f.prerelease[tag] = prerelease
	return nil
}

func (f *fakeReleaseOps) UploadReleaseAssets(_ context.Context, _ github.Repo, tag string, files []string, replace bool) error {
	release, ok := f.releases[tag]
	if !ok {
		return errors.New("upload to a missing release " + tag)
	}
	f.uploads = append(f.uploads, append([]string(nil), files...))
	f.replaces = append(f.replaces, replace)
	for _, file := range files {
		digest, err := fileDigest(file)
		if err != nil {
			return err
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		release.Assets = append(release.Assets, github.ReleaseAsset{Name: filepath.Base(file), Size: info.Size(), Digest: digest})
	}
	return nil
}

// addRemoteAsset registers an attachment the remote already holds.
func (f *fakeReleaseOps) addRemoteAsset(t *testing.T, tag, path string) {
	t.Helper()
	release, ok := f.releases[tag]
	if !ok {
		t.Fatalf("no release %s", tag)
	}
	digest, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	release.Assets = append(release.Assets, github.ReleaseAsset{Name: filepath.Base(path), Digest: digest})
}

// writes counts every external write the fake performed.
func (f *fakeReleaseOps) writes() int { return len(f.creates) + len(f.uploads) }

// uploaded lists the file names that were uploaded, in order.
func (f *fakeReleaseOps) uploaded() []string {
	var names []string
	for _, batch := range f.uploads {
		for _, file := range batch {
			names = append(names, filepath.Base(file))
		}
	}
	return names
}

type repairFixture struct {
	t    *testing.T
	dir  string
	d    *Deps
	ops  *fakeReleaseOps
	tag  string
	head string
}

// newRepairFixture builds the state an interrupted publish run leaves behind: the
// tag and the release commit are pushed, the uploads are skipped, and creating
// the GitHub Release failed.
func newRepairFixture(t *testing.T, tag, version, channel string) *repairFixture {
	t.Helper()
	dir := newRepo(t)
	d := deps(t, dir)
	d.Cfg.Release.GitHubRelease = true
	d.Options = Options{}

	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, dir, "init", "--bare", "-q", remote)
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	gitRun(t, dir, "tag", "-a", tag, "-m", "Release "+tag)
	gitRun(t, dir, "push", "-q", "origin", "refs/tags/"+tag)

	saveRepairMarker(t, dir, tag, version, channel)
	saveRepairRun(t, dir, nil)

	ops := newFakeReleaseOps()
	d.Releases = ops
	return &repairFixture{t: t, dir: dir, d: d, ops: ops, tag: tag, head: gitOutput(t, dir, "rev-parse", "HEAD")}
}

// withAssets records the attachments the interrupted run built.
func (f *repairFixture) withAssets(files ...string) {
	f.t.Helper()
	run, err := state.LatestRun(f.dir)
	if err != nil || run == nil {
		f.t.Fatalf("latest run: %v run=%v", err, run)
	}
	run.Assets = files
	if err := state.SaveRun(f.dir, run); err != nil {
		f.t.Fatalf("SaveRun: %v", err)
	}
}

// withRun changes the recorded run before the repair reads it.
func (f *repairFixture) withRun(mutate func(*state.RunRecord)) {
	f.t.Helper()
	run, err := state.LatestRun(f.dir)
	if err != nil || run == nil {
		f.t.Fatalf("latest run: %v run=%v", err, run)
	}
	mutate(run)
	if err := state.SaveRun(f.dir, run); err != nil {
		f.t.Fatalf("SaveRun: %v", err)
	}
}

// addRecord writes a change record the recorded release never consumed, which is
// exactly the record a fall-through would bump a version for.
func (f *repairFixture) addRecord(slug string) string {
	f.t.Helper()
	record := "---\ntype: fix\npackages:\n  rel-demo: patch\n---\n\nFix something later.\n"
	path := filepath.Join(f.dir, ".changes", slug+".md")
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		f.t.Fatalf("write record: %v", err)
	}
	return path
}

// requireRecord reports whether the pending record survived, i.e. no version was
// bumped and nothing was consumed.
func requireRecord(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the pending change record was consumed: %v", err)
	}
}

func saveRepairMarker(t *testing.T, dir, tag, version, channel string) {
	t.Helper()
	marker := &state.Marker{
		SchemaVersion: 1,
		Version:       version,
		Mode:          config.ModeCommit,
		Channel:       channel,
		Fingerprint:   "fp-repair",
		Repo:          state.MarkerRepo{Host: "github.com", Owner: "example", Name: "demo"},
		Packages: []state.MarkerPackage{{
			Name: "rel-demo", Path: ".", Resolver: "npm",
			Current: "1.0.0", Next: version, Bump: "minor", Tag: tag, Publish: false,
		}},
		Releases: []state.MarkerRelease{{
			Version: version, Tag: tag, Packages: []string{"rel-demo"}, Prerelease: channel,
		}},
		Changes: []state.MarkerChange{{
			File: ".changes/add-feature.md", Type: "feat", Bump: "minor",
			Packages: []string{"rel-demo"}, Body: "Add a feature.",
		}},
	}
	if err := state.SaveMarker(dir, marker); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}
}

func saveRepairRun(t *testing.T, dir string, mutate func(*state.RunRecord)) {
	t.Helper()
	run := state.NewRunRecord("20240101T000000-1", config.ModeCommit, PhasePublish, "fp-repair")
	run.Mark("publish", state.StatusSkipped, nil)
	run.Mark("tag", state.StatusDone, nil)
	run.Mark("push", state.StatusDone, nil)
	run.Mark("github-release", state.StatusFailed, errors.New("release create failed"))
	if mutate != nil {
		mutate(run)
	}
	if err := state.SaveRun(dir, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
}

func writeAsset(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("payload of "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRepairCreatesMissingReleaseOnce replays TASKS PR-04 scenario "tag pushed,
// creating the release failed" and asserts the resume creates it exactly once.
func TestRepairCreatesMissingReleaseOnce(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	ctx := context.Background()

	plan, handled, err := Repair(ctx, f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled || plan == nil {
		t.Fatalf("handled=%v plan=%v", handled, plan)
	}
	if plan.Phase != PhasePublish || plan.Version.Next != "1.1.0" {
		t.Fatalf("unexpected plan: phase=%q next=%q", plan.Phase, plan.Version.Next)
	}
	if got := strings.Join(f.ops.creates, ","); got != "v1.1.0" {
		t.Fatalf("creates = %q, want v1.1.0", got)
	}
	if f.ops.prerelease["v1.1.0"] {
		t.Fatal("a stable release must not be created as a prerelease")
	}
	if f.ops.titles["v1.1.0"] != "v1.1.0" {
		t.Fatalf("title = %q", f.ops.titles["v1.1.0"])
	}

	// The recorded run is finished, so a second resume performs no writes and no
	// lookups at all.
	queries := len(f.ops.queries)
	_, handled, err = Repair(ctx, f.d)
	if err != nil {
		t.Fatalf("second Repair: %v", err)
	}
	if !handled {
		t.Fatal("a repaired release must stay the subject of the resume")
	}
	if f.ops.writes() != 1 {
		t.Fatalf("writes = %d, want 1", f.ops.writes())
	}
	if len(f.ops.queries) != queries {
		t.Fatalf("second resume looked at the remote again: %v", f.ops.queries)
	}
	run, err := state.LatestRun(f.dir)
	if err != nil || run == nil {
		t.Fatalf("latest run: %v", err)
	}
	if step, ok := run.Steps["github-release-repair"]; !ok || step.Status != state.StatusDone {
		t.Fatalf("repair was not recorded: %+v", run.Steps)
	}
	if step, ok := run.Steps["github-release"]; !ok || step.Status != state.StatusDone {
		t.Fatalf("the release step was not marked done: %+v", run.Steps)
	}
}

// TestRepairKeepsNotesAndPrereleaseFlag pins the recorded decisions: a recovered
// release carries the notes and the prerelease attribute the original run would
// have used.
func TestRepairKeepsNotesAndPrereleaseFlag(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0-alpha.1", "1.1.0-alpha.1", "alpha")
	if _, _, err := Repair(context.Background(), f.d); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !f.ops.prerelease["v1.1.0-alpha.1"] {
		t.Fatal("the recorded prerelease must stay a prerelease")
	}
	notes := f.ops.notes["v1.1.0-alpha.1"]
	if !strings.Contains(notes, "Add a feature.") {
		t.Fatalf("the recorded notes were not reused:\n%s", notes)
	}
}

// TestRepairUploadsOnlyMissingAssets replays TASKS PR-04 scenario "release
// created, one attachment not uploaded": only the missing attachment is sent,
// and a repair never lets the CLI replace an existing one.
func TestRepairUploadsOnlyMissingAssets(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	files := []string{writeAsset(t, "demo_1.1.0.tar.gz"), writeAsset(t, "checksums.txt")}
	f.withAssets(files...)
	f.ops.releases["v1.1.0"] = &github.ReleaseSnapshot{Tag: "v1.1.0"}
	f.ops.addRemoteAsset(t, "v1.1.0", files[0])

	if _, handled, err := Repair(context.Background(), f.d); err != nil || !handled {
		t.Fatalf("Repair: handled=%v err=%v", handled, err)
	}
	if len(f.ops.creates) != 0 {
		t.Fatalf("an existing release must not be created again: %v", f.ops.creates)
	}
	if got := strings.Join(f.ops.uploaded(), ","); got != "checksums.txt" {
		t.Fatalf("uploaded = %q, want only checksums.txt", got)
	}
	for _, replace := range f.ops.replaces {
		if replace {
			t.Fatal("a repair must not let gh replace an existing attachment")
		}
	}
}

func TestRepairStopsOnDigestConflict(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	file := writeAsset(t, "demo_1.1.0.tar.gz")
	f.withAssets(file)
	f.ops.releases["v1.1.0"] = &github.ReleaseSnapshot{
		Tag:    "v1.1.0",
		Assets: []github.ReleaseAsset{{Name: "demo_1.1.0.tar.gz", Digest: "sha256:0000"}},
	}
	_, handled, err := Repair(context.Background(), f.d)
	if err == nil {
		t.Fatal("a same-name attachment with another digest must stop the repair")
	}
	if !handled {
		t.Fatal("the recorded release is still the subject of the resume")
	}
	if !strings.Contains(err.Error(), "demo_1.1.0.tar.gz") {
		t.Fatalf("the message does not name the attachment: %v", err)
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
}

func TestRepairStopsWhenTheRemoteCannotBeRead(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	f.ops.queryFault = &github.ReleaseQuery{Status: github.ReleaseUnauthorized, Cause: "no access to example/demo"}
	_, handled, err := Repair(context.Background(), f.d)
	if err == nil || !strings.Contains(err.Error(), "no access") {
		t.Fatalf("err = %v", err)
	}
	if !handled {
		t.Fatal("an unreadable remote must stop the resume, not plan a release")
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
}

// TestRepairKeepsRecordedSkip covers a release the original run switched off: the
// repair keeps that decision, writes nothing, and does not plan a new version.
func TestRepairKeepsRecordedSkip(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	f.withRun(func(run *state.RunRecord) {
		run.Mark("github-release", state.StatusSkipped, nil)
	})
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled {
		t.Fatal("a skipped release must stop the resume instead of planning a new version")
	}
	if f.ops.writes() != 0 || len(f.ops.queries) != 0 {
		t.Fatalf("writes = %d queries = %v", f.ops.writes(), f.ops.queries)
	}
	requireRecord(t, pending)
}

func TestRepairSkipsWhenTheConfigurationDisablesTheRelease(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	f.d.Cfg.Release.GitHubRelease = false
	if _, handled, err := Repair(context.Background(), f.d); err != nil || !handled {
		t.Fatalf("handled=%v err=%v, want a stop", handled, err)
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
	requireRecord(t, pending)
}

func TestRepairSkipsWhenTheFlagDisablesTheRelease(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	f.d.Options.NoGitHubRelease = true
	if _, handled, err := Repair(context.Background(), f.d); err != nil || !handled {
		t.Fatalf("handled=%v err=%v, want a stop", handled, err)
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
	requireRecord(t, pending)
}

// TestRepairStopsWhenTheRemoteTagIsMissing covers a failed tag push: the local
// tag exists, so planning again would bump the next version and consume its
// record. The resume has to stop and say why.
func TestRepairStopsWhenTheRemoteTagIsMissing(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	gitRun(t, f.dir, "push", "-q", "origin", "--delete", "v1.1.0")
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled {
		t.Fatal("a pushed-nowhere tag must stop the resume instead of planning a new version")
	}
	if f.ops.writes() != 0 || len(f.ops.queries) != 0 {
		t.Fatalf("writes = %d queries = %v", f.ops.writes(), f.ops.queries)
	}
	requireRecord(t, pending)
}

func TestRepairStopsWhenTheRemoteTagPointsElsewhere(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	if err := os.WriteFile(filepath.Join(f.dir, "next.txt"), []byte("next\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, f.dir, "add", "-A")
	gitRun(t, f.dir, "commit", "-qm", "chore: move on")
	gitRun(t, f.dir, "tag", "-f", "-a", "v1.1.0", "-m", "moved")

	_, handled, err := Repair(context.Background(), f.d)
	if err == nil {
		t.Fatal("a remote tag pointing elsewhere must stop the repair")
	}
	if !handled {
		t.Fatal("a conflicting tag must stop the resume instead of planning a release")
	}
	if !strings.Contains(err.Error(), "v1.1.0") {
		t.Fatalf("the message does not name the tag: %v", err)
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
}

// TestRepairKeepsNextVersionRecords covers the repository that already carries
// records for the next version: repairing the recorded release must leave them
// and the committed marker untouched.
func TestRepairKeepsNextVersionRecords(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	record := "---\ntype: fix\npackages:\n  rel-demo: patch\n---\n\nFix something later.\n"
	recordPath := filepath.Join(f.dir, ".changes", "next-version.md")
	if err := os.WriteFile(recordPath, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	markerBefore, err := os.ReadFile(state.MarkerPath(f.dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, handled, err := Repair(context.Background(), f.d); err != nil || !handled {
		t.Fatalf("Repair: handled=%v err=%v", handled, err)
	}
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("the next version record was consumed: %v", err)
	}
	markerAfter, err := os.ReadFile(state.MarkerPath(f.dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(markerBefore) != string(markerAfter) {
		t.Fatal("a repair must not rewrite the release marker")
	}
}

func TestRepairAsksForARebuildWhenAnAssetIsGone(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	f.withAssets(filepath.Join(t.TempDir(), "demo_1.1.0.tar.gz"))
	_, handled, err := Repair(context.Background(), f.d)
	if err == nil {
		t.Fatal("an attachment that is gone locally cannot be proven to belong to the release")
	}
	if !handled {
		t.Fatal("the recorded release is still the subject of the resume")
	}
	if !strings.Contains(err.Error(), "demo_1.1.0.tar.gz") {
		t.Fatalf("the message does not name the attachment: %v", err)
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
}

// TestRepairStopsWhenTheRunNeverCreatedItsTags covers a run record without the tag
// step while the local tag exists: the evidence is incomplete, and planning again
// would bump the next version and consume its record.
func TestRepairStopsWhenTheRunNeverCreatedItsTags(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	f.withRun(func(run *state.RunRecord) {
		delete(run.Steps, "tag")
	})
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled {
		t.Fatal("an untagged run record must stop the resume instead of planning a new version")
	}
	if f.ops.writes() != 0 || len(f.ops.queries) != 0 {
		t.Fatalf("writes = %d queries = %v", f.ops.writes(), f.ops.queries)
	}
	requireRecord(t, pending)
}

// TestRepairStopsWhenTheUploadsArePending is the reviewer's second reproduction:
// the run record lost its publish evidence while a publishable package exists.
// Bumping a version for the pending record would hide the unfinished upload.
func TestRepairStopsWhenTheUploadsArePending(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	f.withRun(func(run *state.RunRecord) {
		delete(run.Steps, "publish")
		run.Publish = map[string]state.PackageState{}
		marker, err := state.LoadMarker(f.dir)
		if err != nil || marker == nil {
			t.Fatalf("marker: %v", err)
		}
		marker.Packages[0].Publish = true
		if err := state.SaveMarker(f.dir, marker); err != nil {
			t.Fatalf("SaveMarker: %v", err)
		}
	})
	log := captureUI(f.d)
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled {
		t.Fatal("an unfinished upload must stop the resume instead of planning a new version")
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
	if !strings.Contains(log.String(), "did not finish its package uploads") {
		t.Fatalf("the unfinished upload was not reported:\n%s", log.String())
	}
	requireRecord(t, pending)
}

// TestRepairStopsWhenTheRunRecordIsGone is the reviewer's first reproduction: a new
// runner without local state must not read the missing record as "never
// published" and bump the next version.
func TestRepairStopsWhenTheRunRecordIsGone(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	pending := f.addRecord("later-fix")
	if err := os.RemoveAll(filepath.Join(f.dir, ".quailegg", "state")); err != nil {
		t.Fatalf("remove run state: %v", err)
	}
	log := captureUI(f.d)
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled {
		t.Fatal("a missing run record must stop the resume instead of planning a new version")
	}
	if f.ops.writes() != 0 || len(f.ops.queries) != 0 {
		t.Fatalf("writes = %d queries = %v", f.ops.writes(), f.ops.queries)
	}
	if !strings.Contains(log.String(), "already tagged") {
		t.Fatalf("the reason was not reported:\n%s", log.String())
	}
	requireRecord(t, pending)
}

// captureUI points the executor output at a buffer so a test can read the
// messages a repair prints.
func captureUI(d *Deps) *bytes.Buffer {
	var out bytes.Buffer
	d.UI = &ui.Printer{Out: &out, Err: &out}
	return &out
}

// TestRepairWritesNothingWhenALaterReleaseFailsVerification pins the two-pass
// order: the first release of the marker is repairable, the second one has no
// remote tag, and nothing at all may be written.
func TestRepairWritesNothingWhenALaterReleaseFailsVerification(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	marker, err := state.LoadMarker(f.dir)
	if err != nil || marker == nil {
		t.Fatalf("marker: %v", err)
	}
	marker.Releases = append(marker.Releases, state.MarkerRelease{Version: "1.2.0", Tag: "v1.2.0", Packages: []string{"rel-demo"}})
	if err := state.SaveMarker(f.dir, marker); err != nil {
		t.Fatalf("SaveMarker: %v", err)
	}

	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if handled {
		t.Fatal("a release without a remote tag must not be the subject of the resume")
	}
	if writes := f.ops.writes(); writes != 0 {
		t.Fatalf("wrote %d time(s) before every release was verified", writes)
	}
}

// TestRepairDoesNotClaimCompletionForAnUnverifiedAsset pins the reporting rule:
// an existing attachment without a digest is never replaced and never counted as
// verified, so the resume must not claim the release is complete.
func TestRepairDoesNotClaimCompletionForAnUnverifiedAsset(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	f.withAssets(writeAsset(t, "demo_1.1.0.tar.gz"))
	f.ops.releases["v1.1.0"] = &github.ReleaseSnapshot{
		Tag:    "v1.1.0",
		Assets: []github.ReleaseAsset{{Name: "demo_1.1.0.tar.gz"}},
	}
	log := captureUI(f.d)
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil || !handled {
		t.Fatalf("Repair: handled=%v err=%v", handled, err)
	}
	if writes := f.ops.writes(); writes != 0 {
		t.Fatalf("writes = %d, want 0", writes)
	}
	output := log.String()
	if !strings.Contains(output, "without a digest") {
		t.Fatalf("the unverified attachment was not reported:\n%s", output)
	}
	if strings.Contains(output, "already has every recorded asset") {
		t.Fatalf("the resume claimed a comparison it did not make:\n%s", output)
	}
}

// TestRepairReportsAPresentReleaseWithoutAttachments pins the other half: a run
// that recorded no attachments compared nothing, so it says the release exists
// instead of claiming it is complete.
func TestRepairReportsAPresentReleaseWithoutAttachments(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	f.ops.releases["v1.1.0"] = &github.ReleaseSnapshot{Tag: "v1.1.0"}
	log := captureUI(f.d)
	if _, handled, err := Repair(context.Background(), f.d); err != nil || !handled {
		t.Fatalf("Repair: handled=%v err=%v", handled, err)
	}
	output := log.String()
	if !strings.Contains(output, "already exists; nothing to upload") {
		t.Fatalf("the present release was not reported:\n%s", output)
	}
	if strings.Contains(output, "already has every recorded asset") {
		t.Fatalf("the resume claimed a comparison it did not make:\n%s", output)
	}
}

// scriptedGit answers git invocations from a table, so the ls-remote parsing can
// be checked against output a test repository cannot produce.
type scriptedGit struct {
	answers map[string]execx.Result
}

func (s scriptedGit) Run(_ context.Context, _ string, name string, args ...string) (execx.Result, error) {
	key := name + " " + strings.Join(args, " ")
	if result, ok := s.answers[key]; ok {
		return result, nil
	}
	return execx.Result{}, errors.New("unexpected command: " + key)
}

// TestRemoteTagCommitOnlySplitsOnTheTab pins the parser: a ref name may contain
// spaces, and a line that is not tab separated is not an answer at all. Neither
// may turn into "there is no such tag" by accident.
func TestRemoteTagCommitOnlySplitsOnTheTab(t *testing.T) {
	ctx := context.Background()
	open := func(tag, lsRemote string) *git.Repo {
		ref := "refs/tags/" + tag
		run := scriptedGit{answers: map[string]execx.Result{
			"git rev-parse --show-toplevel":                   {Stdout: "/repo\n"},
			"git ls-remote origin " + ref + " " + ref + "^{}": {Stdout: lsRemote},
		}}
		repo, err := git.Open(ctx, t.TempDir(), run)
		if err != nil {
			t.Fatalf("git.Open: %v", err)
		}
		return repo
	}

	cases := []struct {
		name   string
		tag    string
		output string
		commit string
		found  bool
	}{
		{name: "lightweight tag", tag: "v1.0.0", output: "abc123\trefs/tags/v1.0.0\n", commit: "abc123", found: true},
		{name: "annotated tag peels", tag: "v1.0.0", output: "tagobject\trefs/tags/v1.0.0\nabc123\trefs/tags/v1.0.0^{}\n", commit: "abc123", found: true},
		// A tag spelled with a space answers with three whitespace-separated
		// fields; only the tab tells the ref name apart from the object id.
		{name: "tag with a space", tag: "demo tag", output: "abc123\trefs/tags/demo tag\n", commit: "abc123", found: true},
		{name: "no tag", tag: "v1.0.0", output: "\n"},
		{name: "not tab separated", tag: "v1.0.0", output: "abc123 refs/tags/v1.0.0\n"},
	}
	for _, tc := range cases {
		commit, found, err := open(tc.tag, tc.output).RemoteTagCommit(ctx, "origin", tc.tag)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if found != tc.found || commit != tc.commit {
			t.Fatalf("%s: commit=%q found=%v, want %q/%v", tc.name, commit, found, tc.commit, tc.found)
		}
	}
}

// TestRepairRequiresTheReleaseStepToHaveFailed pins the evidence rule: a run
// record that never reached the github-release step does not prove that only the
// release is missing, so nothing is created.
func TestRepairRequiresTheReleaseStepToHaveFailed(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	f.withRun(func(run *state.RunRecord) {
		delete(run.Steps, "github-release")
		run.Mark("assets", state.StatusFailed, errors.New("asset build failed"))
	})
	_, handled, err := Repair(context.Background(), f.d)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !handled {
		t.Fatal("a run that never tried to create the release must stop the resume")
	}
	if writes := f.ops.writes(); writes != 0 {
		t.Fatalf("writes = %d, want 0", writes)
	}
	if len(f.ops.creates) != 0 {
		t.Fatalf("creates = %v, want none", f.ops.creates)
	}
}

// TestRepairSettledStopsWithWaitingRecords pins the terminal rule: once the
// recorded release is repaired, a later resume never starts the next version, not
// even when change records are waiting. It names them and points at qegg release.
func TestRepairSettledStopsWithWaitingRecords(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	ctx := context.Background()
	if _, handled, err := Repair(ctx, f.d); err != nil || !handled {
		t.Fatalf("first Repair: handled=%v err=%v", handled, err)
	}
	// Nothing new: the repair stays terminal, with no further remote lookups.
	queries := len(f.ops.queries)
	log := captureUI(f.d)
	if _, handled, err := Repair(ctx, f.d); err != nil || !handled {
		t.Fatalf("settled Repair: handled=%v err=%v", handled, err)
	}
	if len(f.ops.queries) != queries {
		t.Fatalf("the settled resume looked at the remote again: %v", f.ops.queries)
	}
	if !strings.Contains(log.String(), "complete") {
		t.Fatalf("the settled resume did not say the release is complete:\n%s", log.String())
	}

	// A record the recorded release did not consume belongs to the next version:
	// resume names it instead of bumping and consuming it.
	pending := f.addRecord("later-fix")
	log = captureUI(f.d)
	if _, handled, err := Repair(ctx, f.d); err != nil || !handled {
		t.Fatalf("a waiting record must not fall through: handled=%v err=%v", handled, err)
	}
	if writes := f.ops.writes(); writes != 1 {
		t.Fatalf("writes = %d, want only the first repair", writes)
	}
	if output := log.String(); !strings.Contains(output, "qegg release") || !strings.Contains(output, "1 waiting") {
		t.Fatalf("the waiting record was not reported:\n%s", output)
	}
	requireRecord(t, pending)
}

// TestRepairWorksWhenMainMovedPastTheTag keeps the tag comparison honest: the
// recorded tag stays at the release commit while main moves on, so a repair that
// compared HEAD instead of the tag would refuse a valid release.
func TestRepairWorksWhenMainMovedPastTheTag(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	if err := os.WriteFile(filepath.Join(f.dir, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitRun(t, f.dir, "add", "-A")
	gitRun(t, f.dir, "commit", "-qm", "feat: later work")

	_, handled, err := Repair(context.Background(), f.d)
	if err != nil || !handled {
		t.Fatalf("Repair: handled=%v err=%v", handled, err)
	}
	if got := strings.Join(f.ops.creates, ","); got != "v1.1.0" {
		t.Fatalf("creates = %q, want v1.1.0", got)
	}
}

// TestRepairStopsWhenTheRemoteTagWasRewritten is the same comparison from the
// other side: the remote holds the tag at another commit, so nothing may be
// written.
func TestRepairStopsWhenTheRemoteTagWasRewritten(t *testing.T) {
	f := newRepairFixture(t, "v1.1.0", "1.1.0", "")
	if err := os.WriteFile(filepath.Join(f.dir, "rewritten.txt"), []byte("rewritten\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitRun(t, f.dir, "add", "-A")
	gitRun(t, f.dir, "commit", "-qm", "chore: moved on")
	gitRun(t, f.dir, "tag", "-a", "v1.1.0-rewritten", "-m", "rewritten")
	gitRun(t, f.dir, "push", "-q", "--force", "origin", "refs/tags/v1.1.0-rewritten:refs/tags/v1.1.0")

	_, handled, err := Repair(context.Background(), f.d)
	if err == nil {
		t.Fatal("a remote tag at another commit must stop the repair")
	}
	if !handled {
		t.Fatal("a conflicting remote tag must stop the resume instead of planning a release")
	}
	if !strings.Contains(err.Error(), "v1.1.0") {
		t.Fatalf("the message does not name the tag: %v", err)
	}
	if f.ops.writes() != 0 {
		t.Fatalf("writes = %d, want 0", f.ops.writes())
	}
}
