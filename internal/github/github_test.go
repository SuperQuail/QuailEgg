package github

import "testing"

// TestUploadArgs pins the two upload modes: the publish path may replace its own
// attachments, while a repair must fail on a name collision instead of hiding a
// digest conflict.
func TestUploadArgs(t *testing.T) {
	repo := Repo{Host: "github.com", Owner: "owner", Name: "repo"}
	files := []string{"/tmp/demo.tar.gz", "/tmp/checksums.txt"}
	cases := []struct {
		name    string
		replace bool
		want    []string
	}{
		{
			name:    "publish replaces",
			replace: true,
			want:    []string{"release", "upload", "v1.0.0", "-R", "owner/repo", "--clobber", "/tmp/demo.tar.gz", "/tmp/checksums.txt"},
		},
		{
			name:    "repair never replaces",
			replace: false,
			want:    []string{"release", "upload", "v1.0.0", "-R", "owner/repo", "/tmp/demo.tar.gz", "/tmp/checksums.txt"},
		},
	}
	for _, tc := range cases {
		got := uploadArgs(repo, "v1.0.0", files, tc.replace)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: uploadArgs = %v, want %v", tc.name, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: uploadArgs[%d] = %q, want %q", tc.name, i, got[i], tc.want[i])
			}
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
