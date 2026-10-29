package release

import (
	"time"

	"github.com/SuperQuail/QuailEgg/internal/changelog"
)

// ChangelogSection renders the changelog section for the plan.
func ChangelogSection(plan *Plan, now time.Time) changelog.Section {
	entries := make([]changelog.Entry, 0, len(plan.Changes))
	for _, change := range plan.Changes {
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
		compareURL = plan.Repo.CompareURL(plan.PreviousTag, plan.Tag)
	}
	return changelog.Section{
		Version:    plan.Version.Next,
		Date:       now,
		Entries:    entries,
		CompareURL: compareURL,
	}
}
