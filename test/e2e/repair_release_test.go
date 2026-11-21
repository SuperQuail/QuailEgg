package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// This file covers the repair a resume performs when only the GitHub Release is
// missing. A local bare repository plays the remote and an in-process server
// plays the GitHub API, so no step touches the network or the installed gh.

// repairRemote is an in-process GitHub release API.
type repairRemote struct {
	mu            sync.Mutex
	releases      map[string]*repairRelease
	creates       int
	requests      int
	acceptCreates bool
}

type repairRelease struct {
	Tag        string        `json:"tag_name"`
	Name       string        `json:"name"`
	Notes      string        `json:"body"`
	Prerelease bool          `json:"prerelease"`
	Assets     []repairAsset `json:"assets"`
}

type repairAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

func newRepairRemote(t *testing.T) *repairRemote {
	t.Helper()
	remote := &repairRemote{releases: map[string]*repairRelease{}}
	server := httptest.NewServer(http.HandlerFunc(remote.handle))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GITHUB_REPOSITORY", "example/demo")
	return remote
}

func (r *repairRemote) handle(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests++
	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/repos/example/demo":
		writeRepairJSON(w, http.StatusOK, map[string]string{"full_name": "example/demo"})
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/repos/example/demo/releases/tags/"):
		tag := strings.TrimPrefix(req.URL.Path, "/repos/example/demo/releases/tags/")
		release, ok := r.releases[tag]
		if !ok {
			writeRepairJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		writeRepairJSON(w, http.StatusOK, release)
	case req.Method == http.MethodPost && req.URL.Path == "/repos/example/demo/releases":
		body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		if !r.acceptCreates {
			writeRepairJSON(w, http.StatusInternalServerError, map[string]string{"message": "release creation is broken"})
			return
		}
		var payload repairRelease
		if err := json.Unmarshal(body, &payload); err != nil {
			writeRepairJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		payload.Assets = nil
		r.releases[payload.Tag] = &payload
		r.creates++
		writeRepairJSON(w, http.StatusCreated, payload)
	default:
		writeRepairJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

func writeRepairJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// allowCreates lets the next release create succeed.
func (r *repairRemote) allowCreates() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acceptCreates = true
}

// counters returns the number of created releases and of served requests.
func (r *repairRemote) counters() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creates, r.requests
}

func (r *repairRemote) release(tag string) (repairRelease, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	release, ok := r.releases[tag]
	if !ok {
		return repairRelease{}, false
	}
	return *release, true
}

// seedRelease inserts a release the way an earlier run would have left it.
func (r *repairRemote) seedRelease(t *testing.T, tag string, assets []string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	release := &repairRelease{Tag: tag, Name: tag}
	for _, path := range assets {
		release.Assets = append(release.Assets, repairAssetFor(t, path))
	}
	r.releases[tag] = release
}

// registerUploads records the attachments uploaded through the fake gh, with the
// digest of the file that was sent.
func (r *repairRemote) registerUploads(t *testing.T, tag string, paths []string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	release, ok := r.releases[tag]
	if !ok {
		t.Fatalf("release %s does not exist", tag)
	}
	for _, path := range paths {
		release.Assets = append(release.Assets, repairAssetFor(t, path))
	}
}

func repairAssetFor(t *testing.T, path string) repairAsset {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read asset %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return repairAsset{Name: filepath.Base(path), Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
}

// installRepairGH puts a stub gh on PATH so an upload never reaches the installed
// CLI. Every argument is appended to a log file the test reads back.
func installRepairGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "gh-upload.log")
	t.Setenv("FAKE_GH_LOG", logPath)
	if runtime.GOOS == "windows" {
		script := "@echo off\r\n:loop\r\nif \"%~1\"==\"\" goto done\r\n(echo %~1)>>\"%FAKE_GH_LOG%\"\r\nshift\r\ngoto loop\r\n:done\r\nexit /b 0\r\n"
		if err := os.WriteFile(filepath.Join(dir, "gh.cmd"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\nfor arg in \"$@\"; do\n  echo \"$arg\" >> \"$FAKE_GH_LOG\"\ndone\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// uploadedFiles lists the asset paths the stub gh was asked to upload.
func uploadedFiles(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("gh log: %v", err)
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(filepath.ToSlash(line), "/.quailegg/assets/") {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}

// repairConfig keeps the release offline: one npm package that never publishes,
// GitHub releases on, commit mode.
const repairConfig = `schema_version = 1

[project]
default_branch = "main"

[git]
tag_prefix = "v"
remote = "origin"

[release]
mode = "commit"
github_release = true

[changes]
dir = ".changes"
scope = ["cmd", "internal", "test"]

[verify]
commands = []
timeout = "5m"

[publish]
registries = ["npm"]

[packages."e2e-pkg"]
path = "."
resolver = "npm"
publish = false
`

type repairSetup struct {
	dir     string
	bin     string
	remote  *repairRemote
	logPath string
	tag     string
	assets  []string
}

// newRepairSetup prepares the state TASKS PR-04 describes: the tag is pushed and
// creating the GitHub Release failed.
func newRepairSetup(t *testing.T) *repairSetup {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	remote := newRepairRemote(t)
	logPath := installRepairGH(t)
	bin := buildBinary(t)
	dir := newRepo(t)

	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", origin)
	git(t, dir, "remote", "add", "origin", origin)
	git(t, dir, "push", "-q", "origin", "HEAD:refs/heads/main")
	if err := os.WriteFile(filepath.Join(dir, ".quailegg.toml"), []byte(repairConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := "---\ntype: feat\npackages:\n  e2e-pkg: minor\n---\n\nAdd a feature.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "add-feature.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: add a feature")

	assetsDir := t.TempDir()
	for _, name := range []string{"demo_1.1.0_linux_amd64.tar.gz", "demo_1.1.0_windows_amd64.zip"} {
		if err := os.WriteFile(filepath.Join(assetsDir, name), []byte("payload of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The release creates the tag, pushes it and then fails on the release.
	out := runExpectFailure(t, bin, dir, "release", "--yes", "--skip-verify", "--assets-dir", assetsDir)
	if !strings.Contains(out, "error:") {
		t.Fatalf("the failure must be reported on stderr:\n%s", out)
	}
	tag := "v1.1.0"
	if remote := gitText(t, dir, "ls-remote", "--tags", "origin", "refs/tags/"+tag); remote == "" {
		t.Fatalf("the release must push the tag, got %q", remote)
	}
	if _, ok := remote.release(tag); ok {
		t.Fatal("the injected failure must leave the release absent")
	}
	if creates, _ := remote.counters(); creates != 0 {
		t.Fatalf("creates = %d, want 0 before the repair", creates)
	}
	if status := runStepStatus(t, dir, "github-release"); status != "failed" {
		t.Fatalf("recorded github-release status = %q, want failed", status)
	}

	assets, err := filepath.Glob(filepath.Join(dir, ".quailegg", "assets", tag, "*"))
	if err != nil || len(assets) != 3 {
		t.Fatalf("built assets = %v err=%v, want two payloads and checksums.txt", assets, err)
	}
	return &repairSetup{dir: dir, bin: bin, remote: remote, logPath: logPath, tag: tag, assets: assets}
}

// runExpectFailure runs qegg and requires a non-zero exit.
func runExpectFailure(t *testing.T, bin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--cwd", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("qegg %v unexpectedly succeeded:\n%s", args, out)
	}
	return string(out)
}

// runStepStatus reads one step status of the recorded run.
func runStepStatus(t *testing.T, dir, step string) string {
	t.Helper()
	stateDir := filepath.Join(dir, ".quailegg", "state")
	pointer, err := os.ReadFile(filepath.Join(stateDir, "latest.json"))
	if err != nil {
		t.Fatalf("latest.json: %v", err)
	}
	var latest struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(pointer, &latest); err != nil {
		t.Fatalf("latest.json: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, latest.RunID+".json"))
	if err != nil {
		t.Fatalf("run record: %v", err)
	}
	var run struct {
		Steps map[string]struct {
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatalf("run record: %v", err)
	}
	return run.Steps[step].Status
}

// TestResumeCreatesTheMissingGitHubRelease covers TASKS PR-04 "tag pushed, the
// release was not created": a new process creates it once, keeps the recorded
// notes and prerelease flag, and a second resume writes nothing.
func TestResumeCreatesTheMissingGitHubRelease(t *testing.T) {
	setup := newRepairSetup(t)
	setup.remote.allowCreates()

	runStrict(t, setup.bin, setup.dir, "resume")

	creates, _ := setup.remote.counters()
	if creates != 1 {
		t.Fatalf("creates = %d, want exactly 1", creates)
	}
	release, ok := setup.remote.release(setup.tag)
	if !ok {
		t.Fatalf("release %s was not created", setup.tag)
	}
	if release.Prerelease {
		t.Fatal("a stable release must not be created as a prerelease")
	}
	if !strings.Contains(release.Notes, "## v1.1.0") || !strings.Contains(release.Notes, "Add a feature.") {
		t.Fatalf("the release notes were not kept:\n%s", release.Notes)
	}
	uploaded := uploadedFiles(t, setup.logPath)
	if len(uploaded) != len(setup.assets) {
		t.Fatalf("uploaded %d assets, want %d: %v", len(uploaded), len(setup.assets), uploaded)
	}
	setup.remote.registerUploads(t, setup.tag, uploaded)

	// The repair is recorded, so a second resume performs no external write and
	// no lookup at all.
	createsBefore, requestsBefore := setup.remote.counters()
	logBefore := len(uploadedFiles(t, setup.logPath))
	runStrict(t, setup.bin, setup.dir, "resume")
	createsAfter, requestsAfter := setup.remote.counters()
	if createsAfter != createsBefore {
		t.Fatalf("the second resume created another release: %d -> %d", createsBefore, createsAfter)
	}
	if requestsAfter != requestsBefore {
		t.Fatalf("the second resume contacted the remote: %d -> %d", requestsBefore, requestsAfter)
	}
	if logAfter := len(uploadedFiles(t, setup.logPath)); logAfter != logBefore {
		t.Fatalf("the second resume uploaded assets: %d -> %d", logBefore, logAfter)
	}
}

// TestResumeUploadsOnlyTheMissingAsset covers TASKS PR-04 "the release exists,
// one attachment was not uploaded": only that attachment is sent, and the
// existing one is never replaced.
func TestResumeUploadsOnlyTheMissingAsset(t *testing.T) {
	setup := newRepairSetup(t)
	setup.remote.allowCreates()
	// Model a run that created the release and uploaded its first attachment.
	present := setup.assets[0]
	setup.remote.seedRelease(t, setup.tag, []string{present})

	runStrict(t, setup.bin, setup.dir, "resume")

	creates, _ := setup.remote.counters()
	if creates != 0 {
		t.Fatalf("creates = %d, want 0 for an existing release", creates)
	}
	uploaded := uploadedFiles(t, setup.logPath)
	if len(uploaded) != len(setup.assets)-1 {
		t.Fatalf("uploaded %v, want the %d missing assets", uploaded, len(setup.assets)-1)
	}
	for _, path := range uploaded {
		if path == present {
			t.Fatalf("the attachment %s was already present and must not be replaced", path)
		}
	}
	setup.remote.registerUploads(t, setup.tag, uploaded)

	createsBefore, requestsBefore := setup.remote.counters()
	logBefore := len(uploadedFiles(t, setup.logPath))
	runStrict(t, setup.bin, setup.dir, "resume")
	if creates, requests := setup.remote.counters(); creates != createsBefore || requests != requestsBefore {
		t.Fatalf("the second resume wrote again: creates %d -> %d, requests %d -> %d", createsBefore, creates, requestsBefore, requests)
	}
	if logAfter := len(uploadedFiles(t, setup.logPath)); logAfter != logBefore {
		t.Fatalf("the second resume uploaded assets: %d -> %d", logBefore, logAfter)
	}
}

// readPackageVersion returns the version of the test package manifest.
func readPackageVersion(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatalf("package.json: %v", err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("package.json: %v", err)
	}
	return manifest.Version
}

// addWaitingRecord commits a change record the recorded release never consumed,
// which is what a fall-through to the normal release path would bump a version for.
func addWaitingRecord(t *testing.T, dir string) string {
	t.Helper()
	record := "---\ntype: fix\npackages:\n  e2e-pkg: patch\n---\n\nFix something later.\n"
	// The release commit consumed the only record, so git removed the empty
	// directory along with it.
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatalf(".changes: %v", err)
	}
	path := filepath.Join(dir, ".changes", "later-fix.md")
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "fix: later fix")
	return path
}

// TestResumeDoesNotReleaseWhenRecordsWait is the data-loss guard at the CLI level:
// after a repair, a waiting change record must survive another resume instead of
// being released as a new version.
func TestResumeDoesNotReleaseWhenRecordsWait(t *testing.T) {
	setup := newRepairSetup(t)
	setup.remote.allowCreates()
	runStrict(t, setup.bin, setup.dir, "resume")

	pending := addWaitingRecord(t, setup.dir)
	head := gitText(t, setup.dir, "rev-parse", "HEAD")
	version := readPackageVersion(t, setup.dir)
	createsBefore, requestsBefore := setup.remote.counters()

	runStrict(t, setup.bin, setup.dir, "resume")

	if after := gitText(t, setup.dir, "rev-parse", "HEAD"); after != head {
		t.Fatalf("resume committed a release: HEAD moved %s -> %s", head, after)
	}
	if after := readPackageVersion(t, setup.dir); after != version {
		t.Fatalf("resume bumped the version: %s -> %s", version, after)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("resume consumed the waiting change record: %v", err)
	}
	if creates, requests := setup.remote.counters(); creates != createsBefore || requests != requestsBefore {
		t.Fatalf("resume wrote again: creates %d -> %d, requests %d -> %d", createsBefore, creates, requestsBefore, requests)
	}
}

// TestResumeDoesNotReleaseWhenTheRunRecordIsGone is the same guarantee for a new
// runner: without local state the missing record must not read as "never
// published", so the recorded release is not replaced by a new version.
func TestResumeDoesNotReleaseWhenTheRunRecordIsGone(t *testing.T) {
	setup := newRepairSetup(t)
	setup.remote.allowCreates()
	pending := addWaitingRecord(t, setup.dir)
	if err := os.RemoveAll(filepath.Join(setup.dir, ".quailegg", "state")); err != nil {
		t.Fatalf("remove run state: %v", err)
	}
	head := gitText(t, setup.dir, "rev-parse", "HEAD")
	version := readPackageVersion(t, setup.dir)
	createsBefore, _ := setup.remote.counters()

	out := runStrict(t, setup.bin, setup.dir, "resume")

	if after := gitText(t, setup.dir, "rev-parse", "HEAD"); after != head {
		t.Fatalf("resume committed a release: HEAD moved %s -> %s", head, after)
	}
	if after := readPackageVersion(t, setup.dir); after != version {
		t.Fatalf("resume bumped the version: %s -> %s", version, after)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("resume consumed the waiting change record: %v", err)
	}
	if creates, _ := setup.remote.counters(); creates != createsBefore {
		t.Fatalf("resume created a release: %d -> %d", createsBefore, creates)
	}
	if !strings.Contains(out, "qegg release") {
		t.Fatalf("the output does not point at qegg release:\n%s", out)
	}
}
