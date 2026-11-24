package release

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// ledgerRepo builds a repository with a bare origin and returns both paths.
func ledgerRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	execx.SanitizeGitHookEnv()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "ledger@example.com")
	runGit(t, root, "config", "user.name", "ledger")
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\n  \"name\": \"ledger-demo\",\n  \"version\": \"1.0.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "init")
	remote := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, root, "init", "--bare", "-q", remote)
	runGit(t, root, "remote", "add", "origin", remote)
	runGit(t, root, "push", "-q", "origin", "HEAD:refs/heads/main")
	return root, remote
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func ledgerEntryFor(repo, version string) *state.LedgerEntry {
	identity := state.ReleaseIdentity{Repo: repo, Tags: []string{"v" + version}, Version: version, Mode: "commit", Fingerprint: "sha256:" + version}
	return &state.LedgerEntry{Key: identity.Key(), Identity: identity, Packages: map[string]*state.PackageRecord{}}
}

func TestGitLedgerStoreUsesCheckoutCredentials(t *testing.T) {
	root, remote := ledgerRepo(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GCM_INTERACTIVE", "never")
	runGit(t, root, "--git-dir", remote, "config", "http.receivepack", "true")
	backend := &cgi.Handler{
		Path: git,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(remote), "GIT_HTTP_EXPORT_ALL=1"},
	}
	authorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("checkout:ledger-test"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != authorization {
			w.Header().Set("WWW-Authenticate", "Basic realm=ledger")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	defer server.Close()

	url := server.URL + "/" + filepath.Base(remote)
	runGit(t, root, "remote", "set-url", "origin", url)
	// Match the checkout action's URL-scoped extra header in the local Git config.
	runGit(t, root, "config", "--local", "http."+server.URL+"/.extraHeader", "Authorization: "+authorization)
	runGit(t, root, "fetch", "--no-tags", "origin", "refs/heads/main")
	fetchHead := runGit(t, root, "rev-parse", "FETCH_HEAD")
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("keep this staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "staged.txt")
	indexTree := runGit(t, root, "write-tree")

	store, err := NewGitLedgerStore(execx.OSRunner{}, root, "origin", url, "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLedger()
	entry := ledgerEntryFor("github.com/example/demo", "1.2.0")
	ledger.Put(entry)
	sha, err := store.Save(ledger, "")
	if err != nil {
		t.Fatalf("save using checkout credentials: %v", err)
	}
	loaded, token, err := store.Load()
	if err != nil {
		t.Fatalf("load using checkout credentials: %v", err)
	}
	if token != sha || loaded.Find(entry.Identity) == nil {
		t.Fatalf("loaded ledger is missing the saved release at %s", sha)
	}
	if _, err := store.Save(loaded, token); err != nil {
		t.Fatalf("update using checkout credentials: %v", err)
	}
	if got := runGit(t, root, "write-tree"); got != indexTree {
		t.Errorf("staged tree changed from %s to %s", indexTree, got)
	}
	if got := runGit(t, root, "rev-parse", "FETCH_HEAD"); got != fetchHead {
		t.Errorf("FETCH_HEAD changed from %s to %s", fetchHead, got)
	}
}

// TestGitLedgerStoreSurvivesANewRunner is the cross-runner guarantee: a second
// clone reads the record an earlier run pushed, and a stale revision is rejected
// so two resumes cannot both claim the same package.
func TestGitLedgerStoreSurvivesANewRunner(t *testing.T) {
	root, remote := ledgerRepo(t)
	store, err := NewGitLedgerStore(execx.OSRunner{}, root, "origin", remote, "v1.2.0")
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	ledger, token, err := store.Load()
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if token != "" || len(ledger.Entries) != 0 {
		t.Fatalf("a remote without a ledger must load empty, got token %q", token)
	}

	entry := ledgerEntryFor("github.com/example/demo", "1.2.0")
	entry.Packages["demo"] = &state.PackageRecord{
		Outcome: state.OutcomeUploaded,
		Key:     state.PackageKey{Repo: "github.com/example/demo", Package: "demo", Version: "1.2.0", Target: "npm"},
	}
	ledger.Put(entry)
	first, err := store.Save(ledger, token)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first == "" {
		t.Fatal("create must return the remote revision")
	}

	// The ref is namespaced per tag and holds the documented file.
	if out := runGit(t, root, "ls-remote", remote, LedgerRefPrefix+"v1.2.0"); !strings.Contains(out, LedgerRefPrefix+"v1.2.0") {
		t.Fatalf("ledger ref is missing from the remote: %q", out)
	}
	shown := runGit(t, root, "--git-dir", remote, "show", first+":"+LedgerPathInRef)
	if !strings.Contains(shown, "\"outcome\": \"uploaded\"") {
		t.Fatalf("ledger file is missing the record:\n%s", shown)
	}

	// A new runner: a fresh store instance over the same remote reads it back.
	fresh, err := NewGitLedgerStore(execx.OSRunner{}, root, "origin", remote, "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, freshToken, err := fresh.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if freshToken != first {
		t.Fatalf("token = %q, want %q", freshToken, first)
	}
	if got := reloaded.Entries[entry.Key]; got == nil || got.Packages["demo"].Outcome != state.OutcomeUploaded {
		t.Fatalf("the record did not survive: %+v", got)
	}

	// Two concurrent writers: the one that read the same revision twice loses.
	stale, staleToken, err := fresh.Load()
	if err != nil {
		t.Fatal(err)
	}
	if staleToken != first {
		t.Fatalf("token = %q, want %q", staleToken, first)
	}
	// The winner writes a real change, so the revision has to move.
	reloaded.Entries[entry.Key].Packages["demo"].Outcome = state.OutcomeSkipped
	updated, err := fresh.Save(reloaded, freshToken)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated == freshToken {
		t.Fatalf("the update did not move the ledger ref (%s)", updated)
	}
	stale.Entries[entry.Key].Packages["demo"].Outcome = state.OutcomeStarted
	if _, err := fresh.Save(stale, staleToken); err == nil {
		t.Fatal("a stale write must be rejected")
	} else if !strings.Contains(err.Error(), state.ErrLedgerConflict.Error()) {
		t.Fatalf("error = %v, want a conflict", err)
	}
}

// TestGitLedgerStoreCreateIsExclusive pins the create-only push: the first run
// claims the release, and a second claim of the same ref is refused.
func TestGitLedgerStoreCreateIsExclusive(t *testing.T) {
	root, remote := ledgerRepo(t)
	first, err := NewGitLedgerStore(execx.OSRunner{}, root, "origin", remote, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGitLedgerStore(execx.OSRunner{}, root, "origin", remote, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}

	ledgerA, tokenA, err := first.Load()
	if err != nil {
		t.Fatal(err)
	}
	ledgerB, tokenB, err := second.Load()
	if err != nil {
		t.Fatal(err)
	}
	ledgerA.Put(ledgerEntryFor("github.com/example/demo", "2.0.0"))
	if _, err := first.Save(ledgerA, tokenA); err != nil {
		t.Fatalf("first create: %v", err)
	}
	ledgerB.Put(ledgerEntryFor("github.com/example/demo/other", "2.0.0"))
	if _, err := second.Save(ledgerB, tokenB); err == nil {
		t.Fatal("the second create must be refused while the first owns the ref")
	}
}

// TestLedgerMessagesRenderCompletely pins the placeholder count of the messages
// the ledger produces: a missing verb shows up as %!v(MISSING) in front of the
// user, and no test that only checks for an error would notice.
func TestLedgerMessagesRenderCompletely(t *testing.T) {
	root, _ := ledgerRepo(t)
	missing := filepath.Join(t.TempDir(), "missing.git")

	if _, err := readRemoteTag(context.Background(), execx.OSRunner{}, root, missing, "v1.0.0"); err == nil {
		t.Fatal("a missing remote must fail the tag lookup")
	} else if strings.Contains(err.Error(), "%!") {
		t.Fatalf("rendered message has a missing verb: %v", err)
	}
	if err := pushTag(context.Background(), execx.OSRunner{}, root, missing, "v1.0.0"); err == nil {
		t.Fatal("a missing remote must fail the tag push")
	} else if strings.Contains(err.Error(), "%!") {
		t.Fatalf("rendered message has a missing verb: %v", err)
	}
	if err := createTagAt(context.Background(), execx.OSRunner{}, root, "bad tag name", "", "x"); err == nil {
		t.Fatal("an invalid tag name must fail")
	} else if strings.Contains(err.Error(), "%!") {
		t.Fatalf("rendered message has a missing verb: %v", err)
	}

	facts := packageFacts{
		record: &state.PackageRecord{Outcome: state.OutcomeStarted, Attempts: 1},
		class:  registry.PrecheckUnknown,
	}
	err := decisionError(PackagePlan{Name: "alpha", Next: "1.1.0"}, "npm", facts, decidePackage(facts))
	if err == nil || strings.Contains(err.Error(), "%!") {
		t.Fatalf("stop message = %v", err)
	}
}
