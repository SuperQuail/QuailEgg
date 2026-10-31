package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPrereleaseLine prepares a repository with one alpha release already
// tagged and a second record waiting for the next prerelease.
func newPrereleaseLine(t *testing.T) (*Deps, string) {
	t.Helper()
	dir := newRepo(t)
	d := deps(t, dir)
	writeLock(t, dir, "alpha", "1.1.0-alpha.1")
	writeMarker(t, dir, "1.1.0-alpha.1", "alpha", map[string]string{".changes/add-feature.md": "Add a feature."})
	gitRun(t, dir, "tag", "v1.1.0-alpha.1")

	record := "---\ntype: feat\npackages:\n  rel-demo: minor\n---\n\nAdd a second feature.\n"
	if err := os.WriteFile(filepath.Join(dir, ".changes", "add-second.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	return d, dir
}

func TestPreReleaseNotesExcludePreviousReleases(t *testing.T) {
	d, _ := newPrereleaseLine(t)
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion || plan.Version.Next != "1.1.0-alpha.2" {
		t.Fatalf("plan = %+v", plan.Version)
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("the plan must keep both records for the stable aggregation, got %d", len(plan.Changes))
	}
	shown := plan.displayChanges()
	if len(shown) != 1 || shown[0].Body != "Add a second feature." {
		t.Fatalf("displayChanges = %+v, want only the second feature", shown)
	}

	body := plan.pullRequestBody(d)
	if !strings.Contains(body, "Add a second feature.") {
		t.Fatalf("the PR body is missing the new entry:\n%s", body)
	}
	if strings.Contains(body, "Add a feature.\n") {
		t.Fatalf("the PR body repeats the previous prerelease entry:\n%s", body)
	}
}

func TestMarkerRecordsPreviousChanges(t *testing.T) {
	d, _ := newPrereleaseLine(t)
	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	marker := plan.markerFile(d)
	if len(marker.Changes) != 2 {
		t.Fatalf("the marker must keep the cumulative changes, got %d", len(marker.Changes))
	}
	if len(marker.PreviousChanges) != 1 || marker.PreviousChanges[0] != ".changes/add-feature.md" {
		t.Fatalf("previous changes = %v", marker.PreviousChanges)
	}
}
