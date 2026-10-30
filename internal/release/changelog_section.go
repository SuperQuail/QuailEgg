package release

import (
	"time"

	"github.com/SuperQuail/QuailEgg/internal/changelog"
)

// ChangelogSections renders one section per release unit.
func ChangelogSections(plan *Plan, now time.Time) []changelog.Section {
	sections := make([]changelog.Section, 0, len(plan.Releases))
	for _, release := range plan.Releases {
		entries := make([]changelog.Entry, 0, len(plan.Changes))
		for _, change := range plan.Changes {
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
