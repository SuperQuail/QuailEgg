package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/github"
)

func TestAuthorLookupSkipsInaccessibleRepository(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	lookup := newAuthorLookup(context.Background(), &Deps{HTTP: srv.Client()}, github.Repo{Host: "github.com", Owner: "acme", Name: "private"})
	if got := lookup.resolve(context.Background(), 7, "sha"); got != "" {
		t.Fatalf("author = %q, want empty", got)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("requests = %d, want 1 (accessibility probe only)", got)
	}
}

func TestAuthorLookupCachesPullRequests(t *testing.T) {
	var pulls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/repos/acme/demo/pulls/7":
			atomic.AddInt32(&pulls, 1)
			_, _ = w.Write([]byte("{\"user\":{\"login\":\"octocat\",\"type\":\"User\"}}"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	lookup := newAuthorLookup(context.Background(), &Deps{HTTP: srv.Client()}, github.Repo{Host: "github.com", Owner: "acme", Name: "demo"})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if got := lookup.resolve(ctx, 7, ""); got != "octocat" {
			t.Fatalf("author = %q, want octocat", got)
		}
	}
	if got := atomic.LoadInt32(&pulls); got != 1 {
		t.Fatalf("pull requests = %d, want 1", got)
	}
}

func TestAuthorLookupStopsAfterFailures(t *testing.T) {
	var commits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo":
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/demo/commits/"):
			atomic.AddInt32(&commits, 1)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	lookup := newAuthorLookup(context.Background(), &Deps{HTTP: srv.Client()}, github.Repo{Host: "github.com", Owner: "acme", Name: "demo"})
	ctx := context.Background()
	for _, sha := range []string{"a", "b", "c"} {
		if got := lookup.resolve(ctx, 0, sha); got != "" {
			t.Fatalf("author = %q, want empty", got)
		}
	}
	if got := atomic.LoadInt32(&commits); got != 2 {
		t.Fatalf("commit requests = %d, want 2", got)
	}
}

func TestBuildResolvesContributorLogin(t *testing.T) {
	dir := newRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://github.com/acme/demo")
	gitRun(t, dir, "commit", "--amend", "-q", "-m", "feat: add feature (#7)")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/demo":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/demo/pulls/7":
			_, _ = w.Write([]byte("{\"user\":{\"login\":\"octocat\",\"type\":\"User\"}}"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	plan, err := Build(context.Background(), deps(t, dir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(plan.Changes))
	}
	if plan.Changes[0].Author != "octocat" {
		t.Fatalf("author = %q, want octocat", plan.Changes[0].Author)
	}
	if plan.Changes[0].PullNumber != 7 {
		t.Fatalf("pull number = %d, want 7", plan.Changes[0].PullNumber)
	}
	if plan.Changes[0].PullURL != "https://github.com/acme/demo/pull/7" {
		t.Fatalf("pull url = %q", plan.Changes[0].PullURL)
	}
}
