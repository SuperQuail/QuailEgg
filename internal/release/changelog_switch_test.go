package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// TestReleaseWithoutChangelog pins the opt-out: the release still bumps the
// version, tags and consumes the records, but never writes or commits
// CHANGELOG.md.
func TestReleaseWithoutChangelog(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	off := false
	d.Cfg.Release.Changelog = &off
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "1.1.0" {
		t.Fatalf("unexpected version: %+v", plan.Version)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("a release with the changelog off must not write CHANGELOG.md (err=%v)", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "1.1.0") {
		t.Fatalf("manifest not bumped:\n%s", manifest)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".changes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "README.md" && strings.HasSuffix(entry.Name(), ".md") {
			t.Fatalf("the record was not consumed: %s", entry.Name())
		}
	}
	exists, err := d.Git.TagExists(ctx, "v1.1.0")
	if err != nil || !exists {
		t.Fatalf("tag v1.1.0 missing: %v", err)
	}
	committed := gitOutput(t, dir, "show", "--name-only", "--format=", "HEAD")
	if strings.Contains(committed, "CHANGELOG.md") {
		t.Fatalf("the release commit staged CHANGELOG.md:\n%s", committed)
	}
	run, err := state.LatestRun(dir)
	if err != nil || run == nil {
		t.Fatalf("run record: %v %v", run, err)
	}
	if got := run.Steps["changelog"].Status; got == state.StatusFailed {
		t.Fatalf("the changelog step failed: %+v", run.Steps["changelog"])
	}
}

// TestPullRequestBodyWithoutChangelog keeps the review list but drops the
// rendered changelog when no release writes one.
func TestPullRequestBodyWithoutChangelog(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	d.Cfg.Release.Mode = config.ModePR
	off := false
	d.Cfg.Release.Changelog = &off

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	body := plan.pullRequestBody(d)
	if strings.Contains(body, "### Changelog") {
		t.Fatalf("the pull request body must not carry a changelog section:\n%s", body)
	}
	if !strings.Contains(body, "### Changes") || !strings.Contains(body, "Add a feature.") {
		t.Fatalf("the pull request body lost the change list:\n%s", body)
	}
	if strings.Contains(body, "qegg:release") {
		t.Fatalf("the pull request body must not contain release markers:\n%s", body)
	}
}

// TestReleaseNotesIgnoreChangelogWhenDisabled pins the fallback: with the
// switch off the notes never come from CHANGELOG.md, so the GitHub Release only
// carries the tag line.
func TestReleaseNotesIgnoreChangelogWhenDisabled(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	section := "<!-- qegg:release v1.1.0 -->\n## v1.1.0 - 2026-01-01\n\n### Features\n\n- Add a feature.\n<!-- qegg:release:end -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n\n"+section), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &executor{d: d, plan: plan, run: state.NewRunRecord("test", d.Cfg.Release.Mode, PhaseVersion, "")}
	if notes, ok := e.releaseNotes(plan.Releases[0]); !ok || !strings.Contains(notes, "Add a feature.") {
		t.Fatalf("the changelog section should be the release notes, got %q ok=%v", notes, ok)
	}

	off := false
	d.Cfg.Release.Changelog = &off
	if notes, ok := e.releaseNotes(plan.Releases[0]); ok || notes != "" {
		t.Fatalf("release notes = %q ok=%v, want the tag-only fallback", notes, ok)
	}
}
