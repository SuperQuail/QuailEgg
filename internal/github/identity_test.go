package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

type fakeRunner struct {
	stdout string
	err    error
	calls  []string
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (execx.Result, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.err != nil {
		return execx.Result{}, f.err
	}
	return execx.Result{Stdout: f.stdout}, nil
}

func TestDiscoverTokenPrefersEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	t.Setenv("GH_TOKEN", "gh-env-token")
	if got := DiscoverToken(context.Background(), nil, "github.com"); got != "env-token" {
		t.Fatalf("token = %q, want env-token", got)
	}
}

func TestDiscoverTokenFallsBackToGHToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-env-token")
	if got := DiscoverToken(context.Background(), nil, ""); got != "gh-env-token" {
		t.Fatalf("token = %q, want gh-env-token", got)
	}
}

func TestDiscoverTokenUsesGHCLI(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	restore := ghLookPath
	ghLookPath = func(string) bool { return true }
	t.Cleanup(func() { ghLookPath = restore })
	run := &fakeRunner{stdout: "cli-token\n"}
	if got := DiscoverToken(context.Background(), run, "ghe.example.com"); got != "cli-token" {
		t.Fatalf("token = %q, want cli-token", got)
	}
	if len(run.calls) != 1 || !strings.Contains(run.calls[0], "--hostname ghe.example.com") {
		t.Fatalf("gh calls = %v", run.calls)
	}
}

func TestDiscoverTokenWithoutGH(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	restore := ghLookPath
	ghLookPath = func(string) bool { return false }
	t.Cleanup(func() { ghLookPath = restore })
	if got := DiscoverToken(context.Background(), &fakeRunner{stdout: "cli-token"}, ""); got != "" {
		t.Fatalf("token = %q, want empty", got)
	}
}

func TestAPIBase(t *testing.T) {
	t.Setenv("GITHUB_API_URL", "")
	if got := apiBase(Repo{Host: "github.com"}); got != "https://api.github.com" {
		t.Fatalf("apiBase(github.com) = %q", got)
	}
	if got := apiBase(Repo{}); got != "https://api.github.com" {
		t.Fatalf("apiBase(empty host) = %q", got)
	}
	if got := apiBase(Repo{Host: "ghe.example.com"}); got != "https://ghe.example.com/api/v3" {
		t.Fatalf("apiBase(ghe) = %q", got)
	}
	t.Setenv("GITHUB_API_URL", "https://ghe.example.com/api/v3/")
	if got := apiBase(Repo{Host: "github.com"}); got != "https://ghe.example.com/api/v3" {
		t.Fatalf("apiBase(GITHUB_API_URL) = %q", got)
	}
}

func newIdentityServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/demo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/repos/acme/private", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/repos/acme/demo/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"user\":{\"login\":\"octocat\",\"type\":\"User\"}}"))
	})
	mux.HandleFunc("/repos/acme/demo/pulls/8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"user\":{\"login\":\"dependabot[bot]\",\"type\":\"Bot\"}}"))
	})
	mux.HandleFunc("/repos/acme/demo/pulls/9", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/repos/acme/demo/commits/abc", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"author\":{\"login\":\"octocat\",\"type\":\"User\"}}"))
	})
	mux.HandleFunc("/repos/acme/demo/commits/def", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"author\":null}"))
	})
	mux.HandleFunc("/repos/acme/demo/commits/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestIdentity(t *testing.T, srv *httptest.Server, repo Repo) *Identity {
	t.Helper()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GH_TOKEN", "")
	return NewIdentity(context.Background(), srv.Client(), nil, repo)
}

func TestIdentityRepoAccessible(t *testing.T) {
	srv := newIdentityServer(t)
	if identity := newTestIdentity(t, srv, Repo{Host: "github.com", Owner: "acme", Name: "demo"}); !identity.RepoAccessible(context.Background()) {
		t.Fatal("public repository should be accessible")
	}
	if identity := newTestIdentity(t, srv, Repo{Host: "github.com", Owner: "acme", Name: "private"}); identity.RepoAccessible(context.Background()) {
		t.Fatal("private repository without access must not be accessible")
	}
}

func TestIdentityPullLogin(t *testing.T) {
	srv := newIdentityServer(t)
	identity := newTestIdentity(t, srv, Repo{Host: "github.com", Owner: "acme", Name: "demo"})
	ctx := context.Background()
	if login, err := identity.PullLogin(ctx, 7); err != nil || login != "octocat" {
		t.Fatalf("PullLogin(7) = (%q, %v)", login, err)
	}
	if login, err := identity.PullLogin(ctx, 8); err != nil || login != "" {
		t.Fatalf("PullLogin(bot) = (%q, %v)", login, err)
	}
	if login, err := identity.PullLogin(ctx, 9); err != nil || login != "" {
		t.Fatalf("PullLogin(404) = (%q, %v)", login, err)
	}
	if login, err := identity.PullLogin(ctx, 0); err != nil || login != "" {
		t.Fatalf("PullLogin(0) = (%q, %v)", login, err)
	}
}

func TestIdentityCommitLogin(t *testing.T) {
	srv := newIdentityServer(t)
	identity := newTestIdentity(t, srv, Repo{Host: "github.com", Owner: "acme", Name: "demo"})
	ctx := context.Background()
	if login, err := identity.CommitLogin(ctx, "abc"); err != nil || login != "octocat" {
		t.Fatalf("CommitLogin(abc) = (%q, %v)", login, err)
	}
	if login, err := identity.CommitLogin(ctx, "def"); err != nil || login != "" {
		t.Fatalf("CommitLogin(unlinked) = (%q, %v)", login, err)
	}
	if _, err := identity.CommitLogin(ctx, "boom"); err == nil {
		t.Fatal("CommitLogin(server error) must return an error")
	}
}

func TestLoginOfIgnoresBots(t *testing.T) {
	if got := loginOf(nil); got != "" {
		t.Fatalf("loginOf(nil) = %q", got)
	}
	if got := loginOf(&apiIdentity{Login: "dependabot[bot]", Type: "Bot"}); got != "" {
		t.Fatalf("bot login = %q", got)
	}
	if got := loginOf(&apiIdentity{Login: "github-actions[bot]", Type: "User"}); got != "" {
		t.Fatalf("bot suffix login = %q", got)
	}
	if got := loginOf(&apiIdentity{Login: "octocat", Type: "User"}); got != "octocat" {
		t.Fatalf("user login = %q", got)
	}
}
