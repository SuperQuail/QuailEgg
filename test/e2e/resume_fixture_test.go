package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	gitrepo "github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// This file resumes a release in process: the runner, the HTTP client and the gh
// CLI are the only places the publish phase touches the outside world, so they
// are faked here and every external write is counted.

// fixtureRepository is the hosted identity every fixture release has. The git
// remote is a local bare repository, so the identity has to come from the
// environment, exactly as it does on a hosted runner.
const fixtureRepository = "example/demo"

// pkgSpec describes one package of a fixture repository.
type pkgSpec struct {
	name     string // configuration key
	dir      string // repository-relative directory, "." for the root
	prev     string // version before the release
	version  string // version being released
	resolver string // npm or java
	publish  bool
}

// fakeCall records one non-git command.
type fakeCall struct {
	name string
	dir  string
	args []string
}

// fakeFailure makes one package command fail.
type fakeFailure struct {
	// notFound simulates a tool that is not installed: the request never left.
	notFound bool
	// err is returned after the request was dispatched.
	err error
}

// fakeRunner serves git for real and stands in for the package tools and gh.
type fakeRunner struct {
	real execx.OSRunner

	mu       sync.Mutex
	calls    []fakeCall
	releases map[string]bool
	failures map[string]*fakeFailure
	// blocked makes the publish command of one directory wait for a channel, so a
	// second resume can run while the first is inside the request.
	blocked map[string]chan struct{}
	entered map[string]chan struct{}
	// uploads counts gh release upload calls, the attachment writes.
	uploads int
	// onPublish runs right after a publish command was recorded, before it is
	// answered. It lets a test look at the ledger from inside the request.
	onPublish func(dir string)
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		releases: map[string]bool{},
		failures: map[string]*fakeFailure{},
		blocked:  map[string]chan struct{}{},
		entered:  map[string]chan struct{}{},
	}
}

// Run implements execx.Runner.
func (r *fakeRunner) Run(ctx context.Context, dir, name string, args ...string) (execx.Result, error) {
	if name == "git" {
		return r.real.Run(ctx, dir, name, args...)
	}
	r.mu.Lock()
	r.calls = append(r.calls, fakeCall{name: name, dir: dir, args: append([]string(nil), args...)})
	blocker := r.blocked[dir]
	entered := r.entered[dir]
	failure := r.failures[dir]
	isPublish := publishing(name, args)
	r.mu.Unlock()

	if isPublish {
		if r.onPublish != nil {
			r.onPublish(dir)
		}
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if blocker != nil {
			<-blocker
		}
	}
	if name == "gh" {
		return r.gh(args)
	}
	if failure != nil && isPublish {
		if failure.notFound {
			return execx.Result{ExitCode: -1}, &execx.Error{
				Name: name, Args: args, Err: exec.ErrNotFound,
			}
		}
		cause := failure.err
		if cause == nil {
			cause = errors.New("connection reset by peer")
		}
		return execx.Result{ExitCode: 1}, &execx.Error{Name: name, Args: args, Err: cause}
	}
	return execx.Result{}, nil
}

// RunEnv forwards the environment to Git and fakes the remaining commands.
func (r *fakeRunner) RunEnv(ctx context.Context, dir string, env []string, name string, args ...string) (execx.Result, error) {
	if name == "git" {
		return r.real.RunEnv(ctx, dir, env, name, args...)
	}
	return r.Run(ctx, dir, name, args...)
}

// gh stands in for the GitHub CLI: a release is created once and a second
// attempt reports the existing release.
func (r *fakeRunner) gh(args []string) (execx.Result, error) {
	if len(args) >= 2 && args[0] == "release" && args[1] == "create" {
		tag := args[2]
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.releases[tag] {
			return execx.Result{Stderr: "release already exists\n", ExitCode: 1}, errors.New("gh release create: already exists")
		}
		r.releases[tag] = true
		return execx.Result{}, nil
	}
	if len(args) >= 2 && args[0] == "release" && args[1] == "upload" {
		r.mu.Lock()
		r.uploads++
		r.mu.Unlock()
		return execx.Result{}, nil
	}
	return execx.Result{}, nil
}

func publishing(name string, args []string) bool {
	switch {
	case name == "npm" && len(args) > 0 && args[0] == "publish":
		return true
	case (name == "mvn" || name == "mvnw") && len(args) > 0 && args[len(args)-1] == "deploy":
		return true
	}
	return false
}

// publishes counts the publish commands of one package directory.
func (r *fakeRunner) publishes(dir string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, call := range r.calls {
		if call.dir == dir && publishing(call.name, call.args) {
			count++
		}
	}
	return count
}

// uploadCount counts the gh release upload calls, one per uploaded attachment set.
func (r *fakeRunner) uploadCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uploads
}

// releaseCount counts the GitHub Releases created so far.
func (r *fakeRunner) releaseCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.releases)
}

// fakeRegistry answers the registry queries.
type fakeRegistry struct {
	mu     sync.Mutex
	status map[string]int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{status: map[string]int{}}
}

func (f *fakeRegistry) set(name, version string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[name+"@"+version] = code
}

// RoundTrip implements http.RoundTripper.
func (f *fakeRegistry) RoundTrip(req *http.Request) (*http.Response, error) {
	segments := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	key := segments[len(segments)-2] + "@" + segments[len(segments)-1]
	f.mu.Lock()
	code, ok := f.status[key]
	f.mu.Unlock()
	if !ok {
		code = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

// publishFixture is a repository whose release commit is already in place and
// whose origin is a bare repository.
type publishFixture struct {
	t        *testing.T
	root     string
	remote   string
	branch   string
	cfg      *config.Config
	specs    []pkgSpec
	runner   *fakeRunner
	registry *fakeRegistry
	// legacyMarker writes the release intent an older qegg produced.
	legacyMarker bool
	// seededKey is the ledger key of the seeded release, so lookups never depend
	// on map order or on a silently different identity.
	seededKey string
}

func newPublishFixture(t *testing.T, mode string, specs ...pkgSpec) *publishFixture {
	t.Helper()
	return buildPublishFixture(t, mode, false, specs...)
}

// newLegacyPublishFixture writes the marker an older qegg produced (schema 1):
// such a release has no publish record, so a resume cannot tell a first attempt
// from a lost one.
func newLegacyPublishFixture(t *testing.T, specs ...pkgSpec) *publishFixture {
	t.Helper()
	return buildPublishFixture(t, config.ModeCommit, true, specs...)
}

func buildPublishFixture(t *testing.T, mode string, legacy bool, specs ...pkgSpec) *publishFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	sanitizeGitEnv()
	// The release identity falls back to GITHUB_REPOSITORY when the remote is not
	// a hosted URL, and the repair paths need a repository identity. Pinning it
	// here keeps the fixture independent of the CI environment and gives every
	// checkout of the same fixture the same identity.
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_SERVER_URL", "")
	t.Setenv("GITHUB_REPOSITORY", fixtureRepository)
	// The gh path is taken when the API base is github.com; this fixture stands in
	// for the gh CLI, so it must keep the real base (offlineGitHub would send the
	// release to the REST API instead).
	onlineGitHub(t)
	// Registry backoff would add 22 seconds to a failing query.
	restore := ecosystem.SetRetryWait(nil)
	t.Cleanup(restore)

	stubGH(t)
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.email", "publish@example.com")
	git(t, root, "config", "user.name", "publish")

	f := &publishFixture{
		t: t, root: root, specs: specs, legacyMarker: legacy,
		runner: newFakeRunner(), registry: newFakeRegistry(),
	}
	f.cfg = config.Default()
	f.cfg.Release.Mode = mode
	f.cfg.Release.GitHubRelease = true
	f.cfg.Verify.Commands = nil
	f.cfg.Verify.Timeout = "5m"
	if len(specs) > 1 {
		// A release that bumps several versions cannot use the single-version
		// commit message template.
		f.cfg.Release.CommitMessage = "chore(release): {{versions}}"
	}
	f.cfg.Packages = map[string]config.Package{}
	for _, spec := range specs {
		publishable := spec.publish
		f.cfg.Packages[spec.name] = config.Package{
			Name: spec.name, Path: spec.dir, Resolver: spec.resolver, Publish: &publishable,
		}
		f.writeManifest(spec)
	}
	if err := os.MkdirAll(filepath.Join(root, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".changes", "README.md"), []byte("# Change records\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The first commit is the state before the release: a conflicting tag can be
	// placed on it later.
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "feat: add everything")
	f.branch = strings.TrimSpace(gitText(t, root, "rev-parse", "--abbrev-ref", "HEAD"))
	f.cfg.Project.DefaultBranch = f.branch

	// The release commit: the manifests already carry the new versions and the
	// marker describes the pending release.
	for _, spec := range specs {
		f.writeManifest(spec)
	}
	if err := state.SaveMarker(root, f.markerFile()); err != nil {
		t.Fatalf("marker: %v", err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "chore(release): release")

	f.remote = filepath.Join(t.TempDir(), "origin.git")
	git(t, root, "init", "--bare", "-q", f.remote)
	git(t, root, "remote", "add", "origin", f.remote)
	git(t, root, "push", "-q", "origin", "HEAD:refs/heads/"+f.branch)
	f.seedPlannedRecord()
	return f
}

// seedPlannedRecord writes the record the version phase leaves behind for a
// release it planned: every package is recorded as not started. That record is
// what makes a first deploy legal for an ecosystem that cannot query its
// registry, and it is what a later resume treats as "was recorded, was lost"
// when the ref disappears.
func (f *publishFixture) seedPlannedRecord() {
	f.t.Helper()
	if f.legacyMarker {
		return
	}
	identity := state.ReleaseIdentity{
		SchemaVersion: 1,
		Repo:          "github.com/" + fixtureRepository,
		Tags:          f.releaseTags(),
		Version:       f.markerVersion(),
		Mode:          f.cfg.Release.Mode,
		Fingerprint:   "sha256:fixture",
	}
	entry := &state.LedgerEntry{Key: identity.Key(), Identity: identity, Packages: map[string]*state.PackageRecord{}}
	for _, spec := range f.specs {
		entry.Packages[spec.name] = &state.PackageRecord{
			Key: state.PackageKey{
				Repo: "github.com/" + fixtureRepository, Package: spec.name, Version: spec.version,
				Target: registry.PublishTarget(spec.resolver),
			},
			Resolver: spec.resolver, Path: spec.dir,
			Outcome: state.OutcomeNotStarted,
		}
	}
	ledger := state.NewLedger()
	ledger.Put(entry)
	f.seededKey = entry.Key
	store, err := release.NewGitLedgerStore(execx.OSRunner{}, f.root, "origin", f.remote, f.ledgerTag())
	if err != nil {
		f.t.Fatalf("ledger store: %v", err)
	}
	if _, err := store.Save(ledger, ""); err != nil {
		f.t.Fatalf("seed the planned record: %v", err)
	}
}

// releaseTags lists the release tags of the fixture in a stable order.
func (f *publishFixture) releaseTags() []string {
	tags := make([]string, 0, len(f.specs))
	for _, spec := range f.specs {
		tags = append(tags, f.tag(spec))
	}
	return state.ReleaseSet(tags)
}

// markerVersion is the version the marker carries; a multi-package release has
// no single one.
func (f *publishFixture) markerVersion() string {
	if len(f.specs) == 1 {
		return f.specs[0].version
	}
	return ""
}

func (f *publishFixture) writeManifest(spec pkgSpec) {
	f.t.Helper()
	dir := filepath.Join(f.root, filepath.FromSlash(spec.dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	switch spec.resolver {
	case "java":
		pom := "<project>\n  <modelVersion>4.0.0</modelVersion>\n  <groupId>com.example</groupId>\n" +
			"  <artifactId>" + spec.name + "</artifactId>\n  <version>" + spec.version + "</version>\n</project>\n"
		if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(pom), 0o644); err != nil {
			f.t.Fatal(err)
		}
	default:
		manifest := "{\n  \"name\": \"" + spec.name + "\",\n  \"version\": \"" + spec.version + "\"\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// markerFile renders the committed release intent of the fixture.
func (f *publishFixture) markerFile() *state.Marker {
	schema := state.MarkerSchemaVersion
	if f.legacyMarker {
		schema = 1
	}
	marker := &state.Marker{
		SchemaVersion: schema,
		Mode:          f.cfg.Release.Mode,
		Fingerprint:   "sha256:fixture",
	}
	if len(f.specs) == 1 {
		spec := f.specs[0]
		marker.Version = spec.version
	}
	for _, spec := range f.specs {
		marker.Packages = append(marker.Packages, state.MarkerPackage{
			Name: spec.name, Path: spec.dir, Resolver: spec.resolver,
			Current: spec.prev, Next: spec.version, Bump: "minor", Publish: spec.publish,
		})
		unit := state.MarkerRelease{
			Version: spec.version, Tag: f.tag(spec), Packages: []string{spec.name},
		}
		if len(f.specs) > 1 {
			unit.Name = spec.name
		}
		marker.Releases = append(marker.Releases, unit)
	}
	return marker
}

// tag renders the release tag of one package.
func (f *publishFixture) tag(spec pkgSpec) string {
	f.t.Helper()
	tag, err := release.TagFor(f.cfg, f.cfg.Release.VersionScope, ecosystem.Package{
		Name: spec.name, Identity: spec.name, Path: spec.dir, Resolver: spec.resolver,
	}, spec.version)
	if err != nil {
		f.t.Fatalf("tag for %s: %v", spec.name, err)
	}
	return tag
}

// dir returns the absolute directory of one package.
func (f *publishFixture) dir(spec pkgSpec) string {
	return filepath.Join(f.root, filepath.FromSlash(spec.dir))
}

// appFor builds the use case over one checkout.
func (f *publishFixture) appFor(root string) *app.App {
	f.t.Helper()
	repo, err := gitrepo.Open(context.Background(), root, execx.OSRunner{})
	if err != nil {
		f.t.Fatalf("git.Open: %v", err)
	}
	return &app.App{
		Root: root, Cfg: f.cfg, Run: f.runner, Git: repo, UI: ui.Discard(),
		Now: time.Now, HTTP: &http.Client{Transport: f.registry, Timeout: 5 * time.Second},
	}
}

// resume runs qegg resume in the fixture checkout.
func (f *publishFixture) resume(opts release.Options) error {
	_, err := f.appFor(f.root).Resume(context.Background(), opts)
	return err
}

// resumeAt runs qegg resume in another checkout of the same repository.
func (f *publishFixture) resumeAt(root string, opts release.Options) error {
	_, err := f.appFor(root).Resume(context.Background(), opts)
	return err
}

// clone creates the checkout a new CI runner would use.
func (f *publishFixture) clone() string {
	f.t.Helper()
	dir := filepath.Join(f.t.TempDir(), "runner2")
	git(f.t, filepath.Dir(dir), "clone", "-q", f.remote, dir)
	git(f.t, dir, "config", "user.email", "runner2@example.com")
	git(f.t, dir, "config", "user.name", "runner2")
	if _, err := os.Stat(filepath.Join(dir, ".quailegg", "state")); !os.IsNotExist(err) {
		f.t.Fatalf("a new runner must start without local release state")
	}
	return dir
}

// ledger reads the record the run pushed to the origin.
func (f *publishFixture) ledger(tag string) *state.Ledger {
	f.t.Helper()
	store, err := release.NewGitLedgerStore(execx.OSRunner{}, f.root, "origin", f.remote, tag)
	if err != nil {
		f.t.Fatalf("ledger store: %v", err)
	}
	ledger, _, err := store.Load()
	if err != nil {
		f.t.Fatalf("ledger load: %v", err)
	}
	return ledger
}

// ledgerTag is the tag whose ref carries the release ledger.
func (f *publishFixture) ledgerTag() string {
	f.t.Helper()
	tags := make([]string, 0, len(f.specs))
	for _, spec := range f.specs {
		tags = append(tags, f.tag(spec))
	}
	sort.Strings(tags)
	return tags[0]
}

// record returns the ledger record of one package. It looks the release up by the
// key of the seeded entry: a different key means the run computed another
// identity, which must fail the test instead of being masked by map order.
func (f *publishFixture) record(pkg string) *state.PackageRecord {
	f.t.Helper()
	ledger := f.ledger(f.ledgerTag())
	keys := make([]string, 0, len(ledger.Entries))
	for key := range ledger.Entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if f.seededKey == "" {
		f.t.Fatalf("the fixture did not seed a release record (ledger keys: %v)", keys)
	}
	entry, ok := ledger.Entries[f.seededKey]
	if !ok {
		f.t.Fatalf("the release record is gone: want key %s, ledger has %v", f.seededKey, keys)
	}
	record, ok := entry.Packages[pkg]
	if !ok {
		f.t.Fatalf("release record %s has no package %q (%v)", f.seededKey, pkg, keys)
	}
	return record
}

// statusReport renders qegg status for this checkout.
func (f *publishFixture) statusReport(offline bool) *app.StatusReport {
	f.t.Helper()
	report, err := f.appFor(f.root).Status(context.Background(), offline)
	if err != nil {
		f.t.Fatalf("status: %v", err)
	}
	return report
}

// ledgerRevision returns the commit the ledger ref points at.
func (f *publishFixture) ledgerRevision() string {
	f.t.Helper()
	return gitText(f.t, f.root, "ls-remote", f.remote, release.LedgerRefPrefix+f.ledgerTag())
}

// remoteTags lists the tags on the bare origin.
func (f *publishFixture) remoteTags() string {
	f.t.Helper()
	return gitText(f.t, f.root, "ls-remote", "--tags", f.remote)
}

// rejectTagPushes installs a pre-receive hook that refuses tag updates only, so
// the ledger ref keeps working. It returns a function that removes the hook.
func (f *publishFixture) rejectTagPushes() func() {
	f.t.Helper()
	return rejectTagPushHook(f.t, f.remote)
}

// rejectTagPushHook installs the tag-refusing hook in a bare repository.
func rejectTagPushHook(t *testing.T, bare string) func() {
	t.Helper()
	hook := filepath.Join(bare, "hooks", "pre-receive")
	script := "#!/bin/sh\n" +
		"status=0\n" +
		"while read old new ref; do\n" +
		"  case \"$ref\" in refs/tags/*) echo \"tag pushes are refused by this test\" >&2; status=1;; esac\n" +
		"done\n" +
		"exit $status\n"
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(hook); err != nil {
			t.Fatalf("remove hook: %v", err)
		}
	}
}

// pointRemoteTag forces one tag on the origin to another commit, simulating a
// conflicting published tag.
func (f *publishFixture) pointRemoteTag(tag, rev string) {
	f.t.Helper()
	git(f.t, f.root, "--git-dir", f.remote, "tag", "-f", tag, rev)
}

// stubGH puts a gh stub first on PATH so the release never falls back to the
// GitHub API: the runner intercepts the gh commands themselves.
func stubGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	script := "#!/bin/sh\nexit 1\n"
	if runtime.GOOS == "windows" {
		path += ".cmd"
		script = "@echo off\r\nexit /b 1\r\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GITHUB_TOKEN", "")
}
