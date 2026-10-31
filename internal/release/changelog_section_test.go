package release

import (
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

func TestPrereleasePublishShowsOnlyNewEntries(t *testing.T) {
	plan := &Plan{
		Phase:   PhasePublish,
		Channel: "alpha",
		Marker:  &state.Marker{PreviousChanges: []string{".changes/old.md"}},
		Changes: []ChangePlan{
			{File: ".changes/old.md", Body: "Old."},
			{File: ".changes/new.md", Body: "New."},
		},
		Releases: []ReleasePlan{{
			Version: "1.1.0-alpha.2", Tag: "v1.1.0-alpha.2", PrevTag: "v1.1.0-alpha.1",
			Packages: []string{"rel-demo"},
		}},
	}
	sections := ChangelogSections(plan, time.Now())
	if len(sections) != 1 || len(sections[0].Entries) != 1 || sections[0].Entries[0].Body != "New." {
		t.Fatalf("sections = %+v, want only the new entry", sections)
	}
}

func TestStableReleaseKeepsAllEntries(t *testing.T) {
	plan := &Plan{
		Phase:  PhasePublish,
		Marker: &state.Marker{PreviousChanges: []string{".changes/old.md"}},
		Changes: []ChangePlan{
			{File: ".changes/old.md", Body: "Old."},
			{File: ".changes/new.md", Body: "New."},
		},
		Releases: []ReleasePlan{{Version: "1.1.0", Tag: "v1.1.0", PrevTag: "v1.0.0"}},
	}
	sections := ChangelogSections(plan, time.Now())
	if len(sections) != 1 || len(sections[0].Entries) != 2 {
		t.Fatalf("sections = %+v, want both entries", sections)
	}
}

func TestOldMarkerWithoutBaselineKeepsAllEntries(t *testing.T) {
	plan := &Plan{
		Phase:   PhasePublish,
		Channel: "alpha",
		Marker:  &state.Marker{},
		Changes: []ChangePlan{
			{File: ".changes/old.md", Body: "Old."},
			{File: ".changes/new.md", Body: "New."},
		},
		Releases: []ReleasePlan{{Version: "1.1.0-alpha.2", Tag: "v1.1.0-alpha.2"}},
	}
	sections := ChangelogSections(plan, time.Now())
	if len(sections) != 1 || len(sections[0].Entries) != 2 {
		t.Fatalf("sections = %+v, want a fallback to every entry", sections)
	}
}
