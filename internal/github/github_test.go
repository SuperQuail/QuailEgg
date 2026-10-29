package github

import "testing"

func TestUploadArgs(t *testing.T) {
	got := uploadArgs("v1.0.0", []string{"/tmp/demo.tar.gz", "/tmp/checksums.txt"})
	want := []string{"release", "upload", "v1.0.0", "--clobber", "/tmp/demo.tar.gz", "/tmp/checksums.txt"}
	if len(got) != len(want) {
		t.Fatalf("uploadArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uploadArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

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
	if got := repo.PullURL(58); got != "https://github.com/owner/repo/pull/58" {
		t.Errorf("PullURL = %q", got)
	}
	if got := repo.PullURL(0); got != "" {
		t.Errorf("PullURL(0) = %q", got)
	}
}
