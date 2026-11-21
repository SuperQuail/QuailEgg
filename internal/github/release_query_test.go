package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// releaseQueryRunner answers gh invocations through a script, so the gh path is
// covered without ever executing the installed CLI.
type releaseQueryRunner struct {
	answer func(args []string) (execx.Result, error)
	calls  []string
}

func (r *releaseQueryRunner) Run(_ context.Context, _ string, name string, args ...string) (execx.Result, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if r.answer == nil {
		return execx.Result{}, nil
	}
	return r.answer(args)
}

// ghScript answers release and repository probes separately.
func ghScript(release, repo execx.Result, releaseErr, repoErr error) *releaseQueryRunner {
	return &releaseQueryRunner{answer: func(args []string) (execx.Result, error) {
		if len(args) > 1 && args[0] == "release" && args[1] == "view" {
			return release, releaseErr
		}
		if len(args) > 1 && args[0] == "repo" && args[1] == "view" {
			return repo, repoErr
		}
		return execx.Result{}, nil
	}}
}

// withGHCLI makes the gh path reachable without executing it.
func withGHCLI(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_API_URL", "")
	restore := ghLookPath
	ghLookPath = func(string) bool { return true }
	t.Cleanup(func() { ghLookPath = restore })
}

func withGHMissing(t *testing.T) {
	t.Helper()
	restore := ghLookPath
	ghLookPath = func(string) bool { return false }
	t.Cleanup(func() { ghLookPath = restore })
}

func demoRepo() Repo { return Repo{Host: "github.com", Owner: "example", Name: "demo"} }

var exitOne = errors.New("exit status 1")

func TestQueryReleaseReportsFoundWithDigests(t *testing.T) {
	withGHCLI(t)
	run := ghScript(execx.Result{Stdout: `{"tagName":"v1.0.0","isPrerelease":true,"body":"notes","name":"v1.0.0","assets":[{"name":"demo.tar.gz","size":12,"digest":"sha256:abc"}]}`}, execx.Result{}, nil, nil)
	query := QueryRelease(context.Background(), run, demoRepo(), "v1.0.0")
	if query.Status != ReleaseFound || !query.Found() {
		t.Fatalf("status = %q, want found", query.Status)
	}
	if !query.Release.Prerelease || query.Release.Notes != "notes" {
		t.Fatalf("unexpected release: %+v", query.Release)
	}
	asset, ok := query.Asset("demo.tar.gz")
	if !ok || asset.Size != 12 || asset.Digest != "sha256:abc" {
		t.Fatalf("unexpected asset: %+v ok=%v", asset, ok)
	}
	if _, ok := query.Asset("other.zip"); ok {
		t.Fatal("a missing asset must not resolve")
	}
	if len(run.calls) != 1 {
		t.Fatalf("gh calls = %v", run.calls)
	}
	for _, want := range []string{"release view v1.0.0", "-R example/demo", "--json " + releaseQueryFields} {
		if !strings.Contains(run.calls[0], want) {
			t.Fatalf("gh call %q is missing %q", run.calls[0], want)
		}
	}
}

// TestQueryReleaseSeparatesMissingFromUnreadable pins the answers a repair
// depends on: only a missing release may be created. A repository the token
// cannot see answers exactly like an absent release, so the probe decides.
func TestQueryReleaseSeparatesMissingFromUnreadable(t *testing.T) {
	withGHCLI(t)
	notFound := execx.Result{Stderr: "release not found", ExitCode: 1}

	visible := ghScript(notFound, execx.Result{Stdout: `{"nameWithOwner":"example/demo"}`}, exitOne, nil)
	query := QueryRelease(context.Background(), visible, demoRepo(), "v1.0.0")
	if query.Status != ReleaseMissing || !query.Missing() {
		t.Fatalf("status = %q, want missing", query.Status)
	}
	if !strings.Contains(query.Cause, "v1.0.0") || !strings.Contains(query.Cause, "github.com/example/demo") {
		t.Fatalf("cause does not name tag and repository: %q", query.Cause)
	}

	hidden := ghScript(notFound, execx.Result{Stderr: "Could not resolve to a Repository with the name 'example/demo'", ExitCode: 1}, exitOne, exitOne)
	query = QueryRelease(context.Background(), hidden, demoRepo(), "v1.0.0")
	if query.Status != ReleaseUnauthorized {
		t.Fatalf("status = %q, want unauthorized", query.Status)
	}
	if !strings.Contains(query.Cause, "example/demo") {
		t.Fatalf("cause does not name the repository: %q", query.Cause)
	}

	failed := ghScript(execx.Result{Stderr: "HTTP 403: Resource not accessible by integration", ExitCode: 1}, execx.Result{}, exitOne, nil)
	query = QueryRelease(context.Background(), failed, demoRepo(), "v1.0.0")
	if query.Status != ReleaseUnverifiable {
		t.Fatalf("status = %q, want unverifiable", query.Status)
	}
	if !strings.Contains(query.Cause, "403") {
		t.Fatalf("cause does not keep the status: %q", query.Cause)
	}
}

func TestQueryReleaseRejectsGarbageAnswers(t *testing.T) {
	withGHCLI(t)
	run := ghScript(execx.Result{Stdout: "not json"}, execx.Result{}, nil, nil)
	query := QueryRelease(context.Background(), run, demoRepo(), "v1.0.0")
	if query.Status != ReleaseUnverifiable || query.Cause == "" {
		t.Fatalf("status = %q cause = %q", query.Status, query.Cause)
	}
	empty := QueryRelease(context.Background(), run, demoRepo(), "  ")
	if empty.Status != ReleaseUnverifiable {
		t.Fatalf("an empty tag must not be looked up, got %q", empty.Status)
	}
	incomplete := QueryRelease(context.Background(), run, Repo{}, "v1.0.0")
	if incomplete.Status != ReleaseUnverifiable {
		t.Fatalf("a repository without owner and name must not be looked up, got %q", incomplete.Status)
	}
}

// TestQueryReleaseWithoutCredentials pins the fallback: without gh a read needs a
// token, and its absence is reported instead of pretending the release is
// missing.
func TestQueryReleaseWithoutCredentials(t *testing.T) {
	t.Setenv("GITHUB_API_URL", "")
	t.Setenv("GITHUB_TOKEN", "")
	withGHMissing(t)
	query := QueryRelease(context.Background(), nil, demoRepo(), "v1.0.0")
	if query.Status != ReleaseUnverifiable || !strings.Contains(query.Cause, "GITHUB_TOKEN") {
		t.Fatalf("status = %q cause = %q", query.Status, query.Cause)
	}
}

// releaseServer serves the release endpoint and the repository endpoint with
// independent answers, and counts every request.
type releaseServer struct {
	*httptest.Server
	requests      int32
	lastBody      string
	releaseStatus int
	releaseBody   string
	repoStatus    int
	repoBody      string
}

func newReleaseServer(t *testing.T, releaseStatus int, releaseBody string) *releaseServer {
	t.Helper()
	server := &releaseServer{
		releaseStatus: releaseStatus,
		releaseBody:   releaseBody,
		repoStatus:    http.StatusOK,
		repoBody:      `{"full_name":"example/demo"}`,
	}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server.requests, 1)
		buffer := new(strings.Builder)
		_, _ = io.Copy(buffer, r.Body)
		server.lastBody = buffer.String()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/releases/tags/"), strings.HasSuffix(r.URL.Path, "/releases"):
			w.WriteHeader(server.releaseStatus)
			_, _ = w.Write([]byte(server.releaseBody))
		case r.URL.Path == "/repos/example/demo":
			w.WriteHeader(server.repoStatus)
			_, _ = w.Write([]byte(server.repoBody))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	return server
}

func TestQueryReleaseUsesRESTForCustomAPIRoot(t *testing.T) {
	// A custom API root means gh's default github.com target would be wrong, so
	// the REST API is used even when gh is installed.
	withGHCLI(t)
	server := newReleaseServer(t, http.StatusOK, `{"tag_name":"v1.0.0","prerelease":true,"body":"notes","assets":[{"name":"demo.tar.gz","size":3,"digest":"sha256:abc"}]}`)
	query := QueryRelease(context.Background(), nil, demoRepo(), "v1.0.0")
	if query.Status != ReleaseFound {
		t.Fatalf("status = %q, want found", query.Status)
	}
	if !query.Release.Prerelease || len(query.Release.Assets) != 1 {
		t.Fatalf("unexpected release: %+v", query.Release)
	}
	if atomic.LoadInt32(&server.requests) != 1 {
		t.Fatalf("requests = %d", server.requests)
	}
}

func TestQueryReleaseRESTStatuses(t *testing.T) {
	withGHCLI(t)
	server := newReleaseServer(t, http.StatusNotFound, `{"message":"Not Found"}`)
	if query := QueryRelease(context.Background(), nil, demoRepo(), "v1.0.0"); query.Status != ReleaseMissing {
		t.Fatalf("release 404 with a visible repository = %q, want missing", query.Status)
	}
	// A 404 on the repository itself means the token cannot see it, so the
	// release is not proven missing.
	server.repoStatus = http.StatusNotFound
	server.repoBody = `{"message":"Not Found"}`
	if query := QueryRelease(context.Background(), nil, demoRepo(), "v1.0.0"); query.Status != ReleaseUnauthorized {
		t.Fatalf("release 404 with a hidden repository = %q, want unauthorized", query.Status)
	}
	server.releaseStatus = http.StatusForbidden
	server.releaseBody = `{"message":"Resource not accessible by integration"}`
	query := QueryRelease(context.Background(), nil, demoRepo(), "v1.0.0")
	if query.Status != ReleaseUnauthorized {
		t.Fatalf("403 status = %q, want unauthorized", query.Status)
	}
	if !strings.Contains(query.Cause, "403") {
		t.Fatalf("cause = %q", query.Cause)
	}
	server.releaseStatus = http.StatusInternalServerError
	server.releaseBody = `{"message":"boom"}`
	if query := QueryRelease(context.Background(), nil, demoRepo(), "v1.0.0"); query.Status != ReleaseUnverifiable {
		t.Fatalf("500 status = %q, want unverifiable", query.Status)
	}
}

// TestCreateReleaseUsesRESTForCustomAPIRoot pins the transport rule on the write
// side too, so a repair can be driven end to end against a local server.
func TestCreateReleaseUsesRESTForCustomAPIRoot(t *testing.T) {
	withGHCLI(t)
	server := newReleaseServer(t, http.StatusCreated, `{"tag_name":"v1.0.0"}`)
	if err := CreateRelease(context.Background(), nil, demoRepo(), "v1.0.0", "title", "notes", true); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if atomic.LoadInt32(&server.requests) != 1 {
		t.Fatalf("requests = %d", server.requests)
	}
	var payload struct {
		TagName    string `json:"tag_name"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal([]byte(server.lastBody), &payload); err != nil {
		t.Fatalf("request body: %v\n%s", err, server.lastBody)
	}
	if payload.TagName != "v1.0.0" || payload.Name != "title" || payload.Body != "notes" || !payload.Prerelease {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	server.releaseStatus = http.StatusUnprocessableEntity
	server.releaseBody = `{"errors":[{"code":"already_exists"}]}`
	if err := CreateRelease(context.Background(), nil, demoRepo(), "v1.0.0", "title", "notes", false); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

func TestCreateReleaseWithoutRepository(t *testing.T) {
	withGHCLI(t)
	if err := CreateRelease(context.Background(), nil, Repo{}, "v1.0.0", "title", "notes", false); err == nil {
		t.Fatal("a create without a repository must fail before anything runs")
	}
}
