// Package github integrates with GitHub releases and pull requests.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// ErrExists reports that the resource already exists.
var ErrExists = errors.New("resource already exists")

// Repo identifies a GitHub repository.
type Repo struct {
	Host  string
	Owner string
	Name  string
}

// Valid reports whether the repository is complete.
func (r Repo) Valid() bool { return r.Owner != "" && r.Name != "" }

// URL returns the repository web URL.
func (r Repo) URL() string {
	if !r.Valid() {
		return ""
	}
	host := r.Host
	if host == "" {
		host = "github.com"
	}
	return "https://" + host + "/" + r.Owner + "/" + r.Name
}

// CommitURL returns the web URL of a commit.
func (r Repo) CommitURL(sha string) string {
	if !r.Valid() || sha == "" {
		return ""
	}
	return r.URL() + "/commit/" + sha
}

// PullURL returns the web URL of a pull request.
func (r Repo) PullURL(number int) string {
	if !r.Valid() || number <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/pull/%d", r.URL(), number)
}

// CompareURL returns the compare URL between two tags.
func (r Repo) CompareURL(previous, next string) string {
	if !r.Valid() || previous == "" || next == "" {
		return ""
	}
	return r.URL() + "/compare/" + previous + "..." + next
}

// ParseRemote extracts a repository from a git remote URL.
func ParseRemote(remote string) (Repo, bool) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, "/")
	remote = strings.TrimSuffix(remote, ".git")

	host := ""
	pathPart := ""
	switch {
	case strings.HasPrefix(remote, "https://"):
		rest := strings.TrimPrefix(remote, "https://")
		host, pathPart = splitHostPath(rest)
	case strings.HasPrefix(remote, "http://"):
		rest := strings.TrimPrefix(remote, "http://")
		host, pathPart = splitHostPath(rest)
	case strings.HasPrefix(remote, "ssh://"):
		rest := strings.TrimPrefix(remote, "ssh://")
		host, pathPart = splitHostPath(rest)
	case strings.HasPrefix(remote, "git@"):
		rest := strings.TrimPrefix(remote, "git@")
		colon := strings.Index(rest, ":")
		if colon < 0 {
			return Repo{}, false
		}
		host = rest[:colon]
		pathPart = rest[colon+1:]
	default:
		return Repo{}, false
	}
	host = strings.TrimSpace(host)
	pathPart = strings.Trim(pathPart, "/")
	parts := strings.Split(pathPart, "/")
	if host == "" || len(parts) < 2 {
		return Repo{}, false
	}
	return Repo{Host: host, Owner: parts[0], Name: strings.Join(parts[1:], "/")}, true
}

func splitHostPath(rest string) (string, string) {
	if at := strings.Index(rest, "@"); at >= 0 && strings.Index(rest, "/") > at {
		rest = rest[at+1:]
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return "", ""
	}
	return rest[:slash], rest[slash+1:]
}

// Detect resolves the repository from the git remote or GITHUB_REPOSITORY.
func Detect(ctx context.Context, run execx.Runner, root, remote string) (Repo, bool) {
	if res, err := run.Run(ctx, root, "git", "remote", "get-url", remote); err == nil {
		if repo, ok := ParseRemote(res.Stdout); ok {
			return repo, true
		}
	}
	if repo, ok := RepoFromEnv(); ok {
		return repo, true
	}
	return Repo{}, false
}

// RepoFromEnv returns the repository from common CI environment variables.
func RepoFromEnv() (Repo, bool) {
	env := os.Getenv("GITHUB_REPOSITORY")
	if env == "" {
		return Repo{}, false
	}
	parts := strings.SplitN(env, "/", 2)
	if len(parts) != 2 {
		return Repo{}, false
	}
	return Repo{Host: "github.com", Owner: parts[0], Name: parts[1]}, true
}

// CreateRelease publishes a GitHub release or reports ErrExists. A prerelease
// is marked as such and never becomes the repository latest release.
func CreateRelease(ctx context.Context, run execx.Runner, repo Repo, tag, title, notes string, prerelease bool) error {
	if execx.LookPath("gh") {
		return createReleaseWithGH(ctx, run, tag, title, notes, prerelease)
	}
	return createReleaseWithAPI(ctx, repo, tag, title, notes, prerelease)
}

func createReleaseWithGH(ctx context.Context, run execx.Runner, tag, title, notes string, prerelease bool) error {
	file, err := os.CreateTemp("", "qegg-release-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(notes); err != nil {
		file.Close()
		return err
	}
	file.Close()
	args := []string{"release", "create", tag, "--title", title, "--notes-file", file.Name()}
	if prerelease {
		args = append(args, "--prerelease", "--latest=false")
	} else {
		args = append(args, "--latest")
	}
	res, err := run.Run(ctx, "", "gh", args...)
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(res.Stderr), "already exists") {
		return ErrExists
	}
	return err
}

// makeLatest maps the prerelease flag to the API make_latest value.
func makeLatest(prerelease bool) string {
	if prerelease {
		return "false"
	}
	return "true"
}

func createReleaseWithAPI(ctx context.Context, repo Repo, tag, title, notes string, prerelease bool) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return errors.New("neither gh nor GITHUB_TOKEN is available")
	}
	if !repo.Valid() {
		return errors.New("cannot determine the GitHub repository")
	}
	payload := map[string]any{
		"tag_name":               tag,
		"name":                   title,
		"body":                   notes,
		"draft":                  false,
		"prerelease":             prerelease,
		"make_latest":            makeLatest(prerelease),
		"generate_release_notes": false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := "https://api.github.com/repos/" + repo.Owner + "/" + repo.Name + "/releases"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusCreated {
		return nil
	}
	if resp.StatusCode == http.StatusUnprocessableEntity && bytes.Contains(responseBody, []byte("already_exists")) {
		return ErrExists
	}
	return fmt.Errorf("github release failed: %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
}

// UploadReleaseAssets attaches files to an existing GitHub release. It is
// idempotent: existing assets with the same names are replaced.
func UploadReleaseAssets(ctx context.Context, run execx.Runner, tag string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	if !execx.LookPath("gh") {
		return errors.New(i18n.T("release.err.assets_gh"))
	}
	_, err := run.Run(ctx, "", "gh", uploadArgs(tag, files)...)
	return err
}

func uploadArgs(tag string, files []string) []string {
	args := []string{"release", "upload", tag, "--clobber"}
	return append(args, files...)
}

// EnsurePR creates or updates a pull request from head into base.
func EnsurePR(ctx context.Context, run execx.Runner, repo Repo, head, base, title, body string) error {
	if !execx.LookPath("gh") {
		return errors.New("gh CLI is required to open a release pull request")
	}
	file, err := os.CreateTemp("", "qegg-pr-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		return err
	}
	file.Close()

	listRes, listErr := run.Run(ctx, "", "gh", "pr", "list", "--head", head, "--base", base, "--state", "open", "--json", "number")
	if listErr == nil {
		var prs []struct {
			Number int
		}
		if err := json.Unmarshal([]byte(listRes.Stdout), &prs); err == nil && len(prs) > 0 {
			_, err := run.Run(ctx, "", "gh", "pr", "edit", fmt.Sprintf("%d", prs[0].Number), "--title", title, "--body-file", file.Name())
			return err
		}
	}
	_, err = run.Run(ctx, "", "gh", "pr", "create", "--head", head, "--base", base, "--title", title, "--body-file", file.Name())
	return err
}
