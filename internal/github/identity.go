package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

const (
	githubAPIBase   = "https://api.github.com"
	tokenTimeout    = 5 * time.Second
	requestTimeout  = 5 * time.Second
	maxResponseBody = 1 << 20
)

// ghLookPath reports whether the gh CLI is installed. Tests replace it.
var ghLookPath = execx.LookPath

// DiscoverToken returns a token for the repository host. The environment wins
// because CI passes GITHUB_TOKEN to the release step; a local gh login is the
// next best source; an empty result means anonymous requests.
func DiscoverToken(ctx context.Context, run execx.Runner, host string) string {
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(key)); token != "" {
			return token
		}
	}
	if run == nil || !ghLookPath("gh") {
		return ""
	}
	args := []string{"auth", "token"}
	if name := strings.TrimSpace(host); name != "" && !strings.EqualFold(name, "github.com") {
		args = append(args, "--hostname", name)
	}
	callCtx, cancel := context.WithTimeout(ctx, tokenTimeout)
	defer cancel()
	res, err := run.Run(callCtx, "", "gh", args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// apiBase resolves the REST API root for a repository.
func apiBase(repo Repo) string {
	if base := strings.TrimSpace(os.Getenv("GITHUB_API_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	host := strings.TrimSpace(repo.Host)
	if host == "" || strings.EqualFold(host, "github.com") {
		return githubAPIBase
	}
	return "https://" + host + "/api/v3"
}

// Identity resolves the GitHub login of commits and pull requests.
type Identity struct {
	client *http.Client
	repo   Repo
	token  string
	base   string
}

// NewIdentity builds a resolver for repo. A nil client or an incomplete
// repository returns nil; callers then treat every login as unresolved.
func NewIdentity(ctx context.Context, client *http.Client, run execx.Runner, repo Repo) *Identity {
	if client == nil || !repo.Valid() {
		return nil
	}
	return &Identity{
		client: client,
		repo:   repo,
		token:  DiscoverToken(ctx, run, repo.Host),
		base:   apiBase(repo),
	}
}

// RepoAccessible reports whether the repository can be read with the current
// credentials. A private repository without a token answers 404; the caller
// then skips every per-change lookup instead of probing each one.
func (i *Identity) RepoAccessible(ctx context.Context) bool {
	if i == nil {
		return false
	}
	_, status, err := i.get(ctx, "/repos/"+i.repo.Owner+"/"+i.repo.Name)
	return err == nil && status == http.StatusOK
}

// PullLogin returns the login of the pull request author, or "" when the
// author is a bot, deleted or unknown.
func (i *Identity) PullLogin(ctx context.Context, number int) (string, error) {
	if i == nil || number <= 0 {
		return "", nil
	}
	path := "/repos/" + i.repo.Owner + "/" + i.repo.Name + "/pulls/" + strconv.Itoa(number)
	body, status, err := i.get(ctx, path)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", nil
	}
	var payload struct {
		User *apiIdentity `json:"user"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode pull request %d: %w", number, err)
	}
	return loginOf(payload.User), nil
}

// CommitLogin returns the login linked to the commit author, or "" when the
// commit email is not linked to an account.
func (i *Identity) CommitLogin(ctx context.Context, sha string) (string, error) {
	if i == nil || sha == "" {
		return "", nil
	}
	path := "/repos/" + i.repo.Owner + "/" + i.repo.Name + "/commits/" + sha
	body, status, err := i.get(ctx, path)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", nil
	}
	var payload struct {
		Author *apiIdentity `json:"author"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode commit %s: %w", sha, err)
	}
	return loginOf(payload.Author), nil
}

type apiIdentity struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// loginOf ignores bots so a release never mentions an automation account.
func loginOf(user *apiIdentity) string {
	if user == nil {
		return ""
	}
	login := strings.TrimSpace(user.Login)
	if login == "" || strings.EqualFold(user.Type, "Bot") || strings.HasSuffix(login, "[bot]") {
		return ""
	}
	return login
}

// get performs one API request. A 404 is an expected answer and not an error.
func (i *Identity) get(ctx context.Context, path string) ([]byte, int, error) {
	if i == nil {
		return nil, 0, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, i.base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "qegg")
	req.Header.Set("Accept", "application/vnd.github+json")
	if i.token != "" {
		req.Header.Set("Authorization", "Bearer "+i.token)
	}
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return body, resp.StatusCode, nil
	case http.StatusNotFound:
		return nil, resp.StatusCode, nil
	default:
		return nil, resp.StatusCode, fmt.Errorf("github api %s: %s", path, resp.Status)
	}
}
