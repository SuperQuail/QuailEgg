package release

import (
	"context"
	"testing"
)

func TestGraduationCompareUsesLastStableTag(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	gitRun(t, dir, "tag", "v1.0.0")
	gitRun(t, dir, "tag", "v1.1.0-alpha.1")
	gitRun(t, dir, "tag", "v1.1.0-alpha.2")
	writeMarker(t, dir, "1.1.0-alpha.2", "alpha", map[string]string{".changes/add-feature.md": "Add a feature."})

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion || !plan.Graduating {
		t.Fatalf("phase = %q graduating = %v", plan.Phase, plan.Graduating)
	}
	if plan.PreviousTag != "v1.0.0" {
		t.Fatalf("PreviousTag = %q, want v1.0.0", plan.PreviousTag)
	}
	if len(plan.Releases) != 1 || plan.Releases[0].PrevTag != "v1.0.0" {
		t.Fatalf("releases = %+v, want previous tag v1.0.0", plan.Releases)
	}
}

func TestGraduationFallsBackToPreReleaseTag(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	gitRun(t, dir, "tag", "v1.1.0-alpha.2")
	writeMarker(t, dir, "1.1.0-alpha.2", "alpha", map[string]string{".changes/add-feature.md": "Add a feature."})

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.PreviousTag != "v1.1.0-alpha.2" {
		t.Fatalf("PreviousTag = %q, want the prerelease fallback", plan.PreviousTag)
	}
}
