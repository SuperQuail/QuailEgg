package release

import (
	"context"

	"github.com/SuperQuail/QuailEgg/internal/github"
)

// authorFailureLimit is the number of failed API calls after which the lookup
// stops trying and every remaining change is rendered without an author.
const authorFailureLimit = 2

// authorLookup resolves contributor logins once per commit or pull request and
// degrades to an empty author when GitHub is unreachable or inaccessible.
type authorLookup struct {
	identity *github.Identity
	pulls    map[int]string
	commits  map[string]string
	failures int
	down     bool
}

func newAuthorLookup(ctx context.Context, d *Deps, repo github.Repo) *authorLookup {
	lookup := &authorLookup{pulls: map[int]string{}, commits: map[string]string{}}
	if d == nil || d.HTTP == nil || !repo.Valid() {
		lookup.down = true
		return lookup
	}
	lookup.identity = github.NewIdentity(ctx, d.HTTP, d.Run, repo)
	if lookup.identity == nil || !lookup.identity.RepoAccessible(ctx) {
		lookup.down = true
	}
	return lookup
}

// resolve returns the login for a change, or "" when it cannot be resolved.
// A pull request number wins over the introducing commit.
func (a *authorLookup) resolve(ctx context.Context, pullNumber int, commit string) string {
	if a == nil || a.down {
		return ""
	}
	if pullNumber > 0 {
		if login, ok := a.pulls[pullNumber]; ok {
			return login
		}
		login, err := a.identity.PullLogin(ctx, pullNumber)
		if err != nil {
			a.fail()
			return ""
		}
		a.pulls[pullNumber] = login
		return login
	}
	if commit == "" {
		return ""
	}
	if login, ok := a.commits[commit]; ok {
		return login
	}
	login, err := a.identity.CommitLogin(ctx, commit)
	if err != nil {
		a.fail()
		return ""
	}
	a.commits[commit] = login
	return login
}

func (a *authorLookup) fail() {
	a.failures++
	if a.failures >= authorFailureLimit {
		a.down = true
	}
}
