package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writePRConfig(t *testing.T, dir string) {
	t.Helper()
	content := `schema_version = 1

[project]
default_branch = "main"

[git]
tag_prefix = "v"
remote = "origin"

[release]
mode = "pr"
github_release = false

[changes]
dir = ".changes"
scope = ["cmd", "internal", "test"]

[verify]
commands = []
timeout = "5m"
fail_fast = true

[publish]
registries = ["go"]

[packages."example.com/demo"]
path = "."
resolver = "go"
publish = false
`
	if err := os.WriteFile(filepath.Join(dir, ".quailegg.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRecord(t *testing.T, dir, slug, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntype: feat\npackages:\n  example.com/demo: minor\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", slug+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// installFakeGH puts a stub gh on PATH so the release PR path can run offline.
func installFakeGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "pr-state")
	log := filepath.Join(dir, "gh.log")
	t.Setenv("FAKE_GH_STATE", state)
	t.Setenv("FAKE_GH_LOG", log)
	if runtime.GOOS == "windows" {
		script := "@echo off\r\necho %*>>\"%FAKE_GH_LOG%\"\r\n" +
			"if \"%1\"==\"pr\" if \"%2\"==\"list\" goto list\r\n" +
			"if \"%1\"==\"pr\" if \"%2\"==\"create\" goto create\r\n" +
			"exit /b 0\r\n" +
			":list\r\n" +
			"if exist \"%FAKE_GH_STATE%\" (echo [{\"number\":7}]) else (echo [])\r\n" +
			"exit /b 0\r\n" +
			":create\r\n" +
			"echo.> \"%FAKE_GH_STATE%\"\r\n" +
			"exit /b 0\r\n"
		if err := os.WriteFile(filepath.Join(dir, "gh.cmd"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\n" +
			"echo \"$@\" >> \"$FAKE_GH_LOG\"\n" +
			"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"list\" ]; then\n" +
			"  if [ -f \"$FAKE_GH_STATE\" ]; then echo '[{\"number\":7}]'; else echo '[]'; fi\n" +
			"  exit 0\n" +
			"fi\n" +
			"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"create\" ]; then : > \"$FAKE_GH_STATE\"; exit 0; fi\n" +
			"exit 0\n"
		if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func remoteHead(t *testing.T, dir, branch string) string {
	t.Helper()
	out := gitText(t, dir, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if out == "" {
		return ""
	}
	return strings.Fields(out)[0]
}

func TestReleasePullRequestRefresh(t *testing.T) {
	bin := buildBinary(t)
	dir := newGoRepo(t)
	git(t, dir, "branch", "-M", "main")
	writePRConfig(t, dir)
	writeRecord(t, dir, "add-feature", "Add a feature.")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: add a feature")

	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "--bare", "-q", origin)
	git(t, dir, "remote", "add", "origin", origin)
	git(t, dir, "push", "-q", "origin", "main")
	mainBefore := remoteHead(t, dir, "main")
	logPath := installFakeGH(t)

	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")
	if remoteHead(t, dir, "release") == "" {
		t.Fatal("release branch was not pushed")
	}
	if got := remoteHead(t, dir, "main"); got != mainBefore {
		t.Fatalf("main moved during version phase: %s -> %s", mainBefore, got)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "pr create") {
		t.Fatalf("expected a created pull request, log:\n%s", log)
	}

	git(t, dir, "checkout", "-q", "main")
	writeRecord(t, dir, "add-second", "Add a second feature.")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat: add a second feature")
	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")

	if got := remoteHead(t, dir, "main"); got != mainBefore {
		t.Fatalf("main moved during the second refresh: %s -> %s", mainBefore, got)
	}
	log, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "pr edit") {
		t.Fatalf("expected the existing pull request to be edited, log:\n%s", log)
	}

	git(t, dir, "fetch", "-q", "origin", "release")
	changelog := gitText(t, dir, "show", "FETCH_HEAD:CHANGELOG.md")
	for _, want := range []string{"## v0.1.0", "Add a feature.", "Add a second feature."} {
		if !strings.Contains(changelog, want) {
			t.Fatalf("release changelog is missing %q:\n%s", want, changelog)
		}
	}

	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "merge", "--squash", "FETCH_HEAD")
	git(t, dir, "commit", "-qm", "chore(release): bump versions")
	git(t, dir, "push", "-q", "origin", "main")
	runStrict(t, bin, dir, "release", "--yes", "--skip-verify", "--no-publish", "--no-github-release")

	head := gitText(t, dir, "rev-parse", "HEAD")
	if got := remoteHead(t, dir, "release"); got != head {
		t.Fatalf("release branch was not synced to main: got %q want %q", got, head)
	}
}
