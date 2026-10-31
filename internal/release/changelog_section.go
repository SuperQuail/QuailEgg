package release

import (
	"time"

	"github.com/SuperQuail/QuailEgg/internal/changelog"
)

// ChangelogSections renders one section per release unit.
func ChangelogSections(plan *Plan, now time.Time) []changelog.Section {
	sections := make([]changelog.Section, 0, len(plan.Releases))
	changes := plan.displayChanges()
	for _, release := range plan.Releases {
		entries := make([]changelog.Entry, 0, len(changes))
		for _, change := range changes {
			if !changeInRelease(change, release) {
				continue
			}
			entries = append(entries, changelog.Entry{
				Type:       change.Type,
				Body:       change.Body,
				CommitSHA:  change.Commit,
				CommitURL:  change.CommitURL,
				PullNumber: change.PullNumber,
				PullURL:    change.PullURL,
				PullAuthor: change.Author,
				SortKey:    change.File,
			})
		}
		compareURL := ""
		if plan.Repo.Valid() {
			compareURL = plan.Repo.CompareURL(release.PrevTag, release.Tag)
		}
		sections = append(sections, changelog.Section{
			Version:    release.Version,
			Package:    release.Name,
			Date:       now,
			Entries:    entries,
			CompareURL: compareURL,
		})
	}
	return sections
}

// displayChanges returns the records that belong to this release's notes.
// Stable releases aggregate every pending record. A prerelease subtracts the
// records earlier prereleases already showed: the previous marker carries them
// during the version phase, and the marker written by that phase carries the
// baseline during the publish phase. Without the baseline the publish run would
// filter the marker against itself and render empty notes.
func (p *Plan) displayChanges() []ChangePlan {
	if p.Channel == "" || p.Marker == nil {
		return p.Changes
	}
	skip := make(map[string]bool)
	if p.Phase == PhasePublish {
		for _, file := range p.Marker.PreviousChanges {
			skip[file] = true
		}
	} else {
		for _, change := range p.Marker.Changes {
			skip[change.File] = true
		}
	}
	if len(skip) == 0 {
		return p.Changes
	}
	shown := make([]ChangePlan, 0, len(p.Changes))
	for _, change := range p.Changes {
		if !skip[change.File] {
			shown = append(shown, change)
		}
	}
	if len(shown) == 0 {
		// An older marker has no baseline; showing the full set is better than
		// publishing an empty release.
		return p.Changes
	}
	return shown
}

// ChangelogSection returns the section of a single-package or shared release.
func ChangelogSection(plan *Plan, now time.Time) changelog.Section {
	sections := ChangelogSections(plan, now)
	if len(sections) == 0 {
		return changelog.Section{Version: plan.Version.Next, Date: now}
	}
	return sections[0]
}

// changeInRelease reports whether a record belongs to a release unit.
func changeInRelease(change ChangePlan, release ReleasePlan) bool {
	if release.Name == "" {
		return true
	}
	if len(change.Packages) == 0 {
		return true
	}
	for _, name := range change.Packages {
		for _, pkg := range release.Packages {
			if name == pkg {
				return true
			}
		}
	}
	return false
}
