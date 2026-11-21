package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// releaseQueryFields lists the JSON fields the gh CLI returns for one release.
// "assets" carries name, size and digest, which is what a repair compares.
const releaseQueryFields = "tagName,isPrerelease,body,name,assets"

// ReleaseStatus classifies the answer of a release lookup. Only ReleaseMissing
// lets a caller create the release: a repository the credentials cannot see
// answers exactly like an absent release, so it has to stop instead.
type ReleaseStatus string

const (
	// ReleaseFound reports that the host returned the release.
	ReleaseFound ReleaseStatus = "found"
	// ReleaseMissing reports that the host definitely has no such release.
	ReleaseMissing ReleaseStatus = "missing"
	// ReleaseUnauthorized reports that the credentials cannot see the
	// repository, so its releases cannot be judged.
	ReleaseUnauthorized ReleaseStatus = "unauthorized"
	// ReleaseUnverifiable reports that the lookup could not answer.
	ReleaseUnverifiable ReleaseStatus = "unverifiable"
)

// ReleaseAsset is one file attached to a release.
type ReleaseAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Digest is "sha256:<hex>" when the host reports one. An empty digest means
	// the answer does not prove which bytes the asset holds.
	Digest string `json:"digest,omitempty"`
}

// ReleaseSnapshot is the release a host reports for one tag.
type ReleaseSnapshot struct {
	Tag        string         `json:"tag_name"`
	Name       string         `json:"name"`
	Notes      string         `json:"body"`
	Prerelease bool           `json:"prerelease"`
	Assets     []ReleaseAsset `json:"assets"`
}

// UnmarshalJSON accepts both spellings of the release payload: the REST API
// answers snake_case ("tag_name", "prerelease") while the gh CLI renames the
// same fields ("tagName", "isPrerelease"). Both feed one snapshot so a caller
// compares the same values whichever transport answered.
func (s *ReleaseSnapshot) UnmarshalJSON(data []byte) error {
	var raw struct {
		TagName      string         `json:"tag_name"`
		TagNameGH    string         `json:"tagName"`
		Name         string         `json:"name"`
		Notes        string         `json:"body"`
		Prerelease   *bool          `json:"prerelease"`
		IsPrerelease *bool          `json:"isPrerelease"`
		Assets       []ReleaseAsset `json:"assets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Tag = raw.TagName
	if s.Tag == "" {
		s.Tag = raw.TagNameGH
	}
	s.Name = raw.Name
	s.Notes = raw.Notes
	s.Assets = raw.Assets
	if raw.Prerelease != nil {
		s.Prerelease = *raw.Prerelease
	}
	if raw.IsPrerelease != nil {
		s.Prerelease = *raw.IsPrerelease
	}
	return nil
}

// ReleaseQuery is the answer of a release lookup.
type ReleaseQuery struct {
	Status  ReleaseStatus
	Release ReleaseSnapshot
	// Cause explains a missing, unauthorized or unverifiable answer in one
	// sentence. It names the repository, the tag and the cause, and never
	// contains credentials.
	Cause string
}

// Found reports whether the release exists.
func (q ReleaseQuery) Found() bool { return q.Status == ReleaseFound }

// Missing reports whether the host definitely has no such release, which is the
// only answer a caller may turn into a create.
func (q ReleaseQuery) Missing() bool { return q.Status == ReleaseMissing }

// Asset looks one attached file up by name.
func (q ReleaseQuery) Asset(name string) (ReleaseAsset, bool) {
	for _, asset := range q.Release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return ReleaseAsset{}, false
}

// useGHCli reports whether the gh CLI can act on this repository. A custom API
// root means the host is not github.com, where gh's default target would be the
// wrong repository, so the REST API is used instead. Tests point GITHUB_API_URL
// at a local server, which also keeps them off the network.
func useGHCli(repo Repo) bool {
	return ghLookPath("gh") && apiBase(repo) == githubAPIBase
}

// QueryRelease reads one release by tag. It prefers the gh CLI and falls back to
// the REST API, the same way CreateRelease does. A repository the credentials
// cannot see answers like an absent release, so both transports ask whether the
// repository is visible before reporting ReleaseMissing.
func QueryRelease(ctx context.Context, run execx.Runner, repo Repo, tag string) ReleaseQuery {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ReleaseQuery{Status: ReleaseUnverifiable, Cause: i18n.T("github.release.no_tag")}
	}
	if !repo.Valid() {
		return ReleaseQuery{Status: ReleaseUnverifiable, Cause: i18n.T("github.release.no_repo")}
	}
	if useGHCli(repo) && run != nil {
		return queryReleaseWithGH(ctx, run, repo, tag)
	}
	return queryReleaseWithAPI(ctx, repo, tag)
}

// queryReleaseWithGH reads a release through the gh CLI. gh answers "release not
// found" both for an absent release and for a repository it cannot see, so the
// repository is probed before that answer counts as missing.
func queryReleaseWithGH(ctx context.Context, run execx.Runner, repo Repo, tag string) ReleaseQuery {
	res, err := run.Run(ctx, "", "gh", "release", "view", tag, "-R", repo.Slug(), "--json", releaseQueryFields)
	if err != nil {
		if !strings.Contains(strings.ToLower(res.Stderr), "release not found") {
			return ReleaseQuery{
				Status: ReleaseUnverifiable,
				Cause:  i18n.Tf("github.release.unverifiable", tag, repo.ID(), causeLine(res.Stderr, err)),
			}
		}
		if probe := ghRepoProbe(ctx, run, repo); probe.Status != "" {
			return probe
		}
		return ReleaseQuery{Status: ReleaseMissing, Cause: i18n.Tf("github.release.missing", repo.ID(), tag)}
	}
	var snapshot ReleaseSnapshot
	if err := json.Unmarshal([]byte(res.Stdout), &snapshot); err != nil {
		return ReleaseQuery{
			Status: ReleaseUnverifiable,
			Cause:  i18n.Tf("github.release.decode", tag, err),
		}
	}
	return foundRelease(tag, snapshot)
}

// ghRepoProbe reports why the repository cannot be read. An empty status means
// the repository is visible, so a "release not found" answer is trustworthy.
func ghRepoProbe(ctx context.Context, run execx.Runner, repo Repo) ReleaseQuery {
	res, err := run.Run(ctx, "", "gh", "repo", "view", repo.Slug(), "--json", "nameWithOwner")
	if err == nil {
		return ReleaseQuery{}
	}
	cause := causeLine(res.Stderr, err)
	lower := strings.ToLower(cause)
	if strings.Contains(lower, "not found") || strings.Contains(lower, "could not resolve to a repository") {
		return ReleaseQuery{
			Status: ReleaseUnauthorized,
			Cause:  i18n.Tf("github.release.unauthorized", repo.ID(), cause),
		}
	}
	return ReleaseQuery{
		Status: ReleaseUnverifiable,
		Cause:  i18n.Tf("github.release.unverified_repo", repo.ID(), cause),
	}
}

// queryReleaseWithAPI reads a release through the REST API. It needs a token: an
// anonymous request answers 404 for a private repository, which would read as
// "missing" and hide an existing release.
func queryReleaseWithAPI(ctx context.Context, repo Repo, tag string) ReleaseQuery {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		return ReleaseQuery{
			Status: ReleaseUnverifiable,
			Cause:  i18n.Tf("github.release.no_token", tag, repo.ID()),
		}
	}
	status, body, err := getJSON(ctx, token, releasesEndpoint(repo)+"/tags/"+url.PathEscape(tag))
	if err != nil {
		return ReleaseQuery{
			Status: ReleaseUnverifiable,
			Cause:  i18n.Tf("github.release.unverifiable", tag, repo.ID(), err),
		}
	}
	switch status {
	case http.StatusOK:
		var snapshot ReleaseSnapshot
		if err := json.Unmarshal(body, &snapshot); err != nil {
			return ReleaseQuery{Status: ReleaseUnverifiable, Cause: i18n.Tf("github.release.decode", tag, err)}
		}
		return foundRelease(tag, snapshot)
	case http.StatusNotFound:
		// A 404 also hides a repository the token cannot see, so ask the
		// repository endpoint before calling the release missing.
		repoStatus, repoBody, repoErr := getJSON(ctx, token, apiBase(repo)+"/repos/"+repo.Owner+"/"+repo.Name)
		if repoErr != nil {
			return ReleaseQuery{
				Status: ReleaseUnverifiable,
				Cause:  i18n.Tf("github.release.unverifiable", tag, repo.ID(), repoErr),
			}
		}
		if repoStatus == http.StatusOK {
			return ReleaseQuery{Status: ReleaseMissing, Cause: i18n.Tf("github.release.missing", repo.ID(), tag)}
		}
		return unauthorized(repo, tag, repoStatus, repoBody)
	case http.StatusUnauthorized, http.StatusForbidden:
		return unauthorized(repo, tag, status, body)
	default:
		return ReleaseQuery{
			Status: ReleaseUnverifiable,
			Cause:  i18n.Tf("github.release.unverifiable", tag, repo.ID(), apiCause(apiStatus(status), body)),
		}
	}
}

// apiStatus renders an HTTP status code for a message.
func apiStatus(status int) string {
	if status <= 0 {
		return "HTTP error"
	}
	return "HTTP " + strconv.Itoa(status)
}

// unauthorized renders a permission problem without echoing the token.
func unauthorized(repo Repo, tag string, status int, body []byte) ReleaseQuery {
	return ReleaseQuery{
		Status: ReleaseUnauthorized,
		Cause:  i18n.Tf("github.release.unauthorized", repo.ID(), apiCause(apiStatus(status), body)),
	}
}

// getJSON performs one authorized GET and returns the status and body. A
// transport failure is an error; every HTTP status is an answer.
func getJSON(ctx context.Context, token, endpoint string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	return resp.StatusCode, body, nil
}

// foundRelease normalizes a decoded answer. A host may omit the tag name.
func foundRelease(tag string, snapshot ReleaseSnapshot) ReleaseQuery {
	if snapshot.Tag == "" {
		snapshot.Tag = tag
	}
	return ReleaseQuery{Status: ReleaseFound, Release: snapshot}
}

// releasesEndpoint returns the REST endpoint of the repository's releases.
func releasesEndpoint(repo Repo) string {
	return apiBase(repo) + "/repos/" + repo.Owner + "/" + repo.Name + "/releases"
}

// apiCause keeps the first line of an error response, which is the part that
// names the problem, prefixed with the HTTP status.
func apiCause(status string, body []byte) string {
	line := firstLine(string(body))
	if line == "" {
		return status
	}
	return status + ": " + line
}

// CauseLine renders a failed command or API call as one line, so a message names
// the cause without dumping the whole output. It never contains credentials.
func CauseLine(err error) string { return causeLine("", err) }

// causeLine renders a failed command as one line without its full output.
func causeLine(stderr string, err error) string {
	line := firstLine(stderr)
	if line == "" && err != nil {
		line = err.Error()
	}
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}

// firstLine returns the first non-empty line, capped so a message stays
// readable in the terminal.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 200 {
			return line[:200]
		}
		return line
	}
	return ""
}
