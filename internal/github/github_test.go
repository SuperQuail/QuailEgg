package github

import "testing"

func TestParseRemote(t *testing.T) {
	cases := []struct {
		remote string
		owner  string
		name   string
		ok     bool
	}{
		{"https://github.com/owner/repo.git", "owner", "repo", true},
		{"https://github.com/owner/repo", "owner", "repo", true},
		{"git@github.com:owner/repo.git", "owner", "repo", true},
		{"ssh://git@github.com/owner/repo.git", "owner", "repo", true},
		{"https://user:token@github.com/owner/repo.git", "owner", "repo", true},
		{"not-a-remote", "", "", false},
	}
	for _, tc := range cases {
		repo, ok := ParseRemote(tc.remote)
		if ok != tc.ok {
			t.Errorf("ParseRemote(%q) ok = %v, want %v", tc.remote, ok, tc.ok)
			continue
		}
		if ok && (repo.Owner != tc.owner || repo.Name != tc.name) {
			t.Errorf("ParseRemote(%q) = %+v", tc.remote, repo)
		}
	}
}

func TestRepoURLs(t *testing.T) {
	repo := Repo{Host: "github.com", Owner: "owner", Name: "repo"}
	if got := repo.URL(); got != "https://github.com/owner/repo" {
		t.Errorf("URL = %q", got)
	}
	if got := repo.CommitURL("abcdef"); got != "https://github.com/owner/repo/commit/abcdef" {
		t.Errorf("CommitURL = %q", got)
	}
	if got := repo.CompareURL("v1.0.0", "v1.1.0"); got != "https://github.com/owner/repo/compare/v1.0.0...v1.1.0" {
		t.Errorf("CompareURL = %q", got)
	}
}
