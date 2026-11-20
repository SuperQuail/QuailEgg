package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// GitHub event names the change record check reads. The generated pull request
// workflow runs the check, but a repository that runs it elsewhere still gets an
// answer: a push names the revisions before and after it, and a merge group names
// the base and head of the queued merge.
const (
	eventPullRequest       = "pull_request"
	eventPullRequestTarget = "pull_request_target"
	eventMergeGroup        = "merge_group"
	eventPush              = "push"
)

// Reasons a CI event leaves nothing to judge. The reason reaches the JSON report,
// so a skipped check is never mistaken for a passing one.
const (
	ciSkipNoEvent       = "no-event"
	ciSkipTagPush       = "tag-push"
	ciSkipDeletedBranch = "deleted-branch"
	ciSkipNewBranch     = "new-branch"
)

// CIReport describes what the change record check judged. The check report
// carries it for qegg check --ci only.
type CIReport struct {
	Event string `json:"event,omitempty"`
	Base  string `json:"base,omitempty"`
	Head  string `json:"head,omitempty"`
	// Skipped names why no record was checked: no-event, tag-push,
	// deleted-branch or new-branch. An empty value means the two revisions of the
	// event were compared.
	Skipped string `json:"skipped,omitempty"`
	// Changed counts the paths the event changed.
	Changed int `json:"changed"`
	// Records lists the records that satisfied the rule.
	Records []string `json:"records,omitempty"`
}

// ciEvent is the revision range one CI event asks the check to judge.
type ciEvent struct {
	Name string // event name, used in problem messages
	Base string
	Head string
	// Skipped names why the event has nothing to judge. An empty value means the
	// check compares Base and Head.
	Skipped string
}

// rangeSpec is the revision range git compares. A pull request and a merge group
// start at the merge base, so changes that landed on the base branch meanwhile
// are not attributed to this change; a push compares its two revisions directly.
func (e ciEvent) rangeSpec() string {
	if e.Name == eventPush {
		return e.Base + ".." + e.Head
	}
	return e.Base + "..." + e.Head
}

// report is the JSON view of the event.
func (e ciEvent) report() *CIReport {
	return &CIReport{Event: e.Name, Base: e.Base, Head: e.Head, Skipped: e.Skipped}
}

// ciRecordProblems judges the snapshot the CI run is about: the tree of the commit
// the event names, which is not the commit this run has checked out. It shares the
// rule with the pre-commit hook, so both judge the same scope, ignore list, record
// set and record content.
func (a *App) ciRecordProblems(ctx context.Context) (*CIReport, []Problem) {
	event, problem := ciEventRange()
	if problem != nil {
		return nil, []Problem{*problem}
	}
	report := event.report()
	if event.Skipped != "" {
		// Nothing to compare. Say why out loud: a silent pass would read as a
		// check that found nothing wrong.
		a.note(ciSkipNote(event.Skipped))
		return report, nil
	}
	files, err := a.Git.ChangedFiles(ctx, event.rangeSpec())
	if err != nil {
		return report, []Problem{ciDiffProblem(*event, err)}
	}
	report.Changed = len(files)
	records, err := changes.Enforce(a.recordSnapshot(changeSet(files), func(rel string) ([]byte, error) {
		return a.Git.TreeBlob(ctx, event.Head, rel)
	}))
	report.Records = records
	if err == nil {
		return report, nil
	}
	var missing *changes.MissingRecordError
	if errors.As(err, &missing) {
		return report, []Problem{{
			Code:    "missing-record",
			Message: i18n.T("check.problem.missing_record"),
			Fix:     i18n.T("check.fix.missing_record"),
		}}
	}
	var invalid *changes.InvalidRecordError
	if errors.As(err, &invalid) {
		// The working tree pass reports the same record with the same wording, so
		// the report keeps one line per broken record.
		return report, []Problem{{
			Code:    "record-invalid",
			Message: invalid.Error(),
			Fix:     i18n.T("check.fix.record_invalid"),
			record:  invalid.Path,
		}}
	}
	return report, []Problem{{
		Code:    "record-rule-failed",
		Message: i18n.Tf("check.problem.record_rule_failed", err),
		Fix:     i18n.T("check.fix.record_rule_failed"),
	}}
}

// note writes one line of context for the run. It goes to stderr, so the JSON
// report on stdout stays machine readable, and a hand-built App without a printer
// simply has nowhere to put it.
func (a *App) note(message string) {
	if a.UI == nil || a.UI.Err == nil {
		return
	}
	fmt.Fprintln(a.UI.Err, message)
}

// ciSkipNote words a skip for the run output.
func ciSkipNote(reason string) string {
	switch reason {
	case ciSkipTagPush:
		return i18n.T("check.ci_skip_tag_push")
	case ciSkipDeletedBranch:
		return i18n.T("check.ci_skip_deleted_branch")
	case ciSkipNewBranch:
		return i18n.T("check.ci_skip_new_branch")
	default:
		return i18n.T("check.ci_skip_no_event")
	}
}

// ciDiffProblem reports a range the check could not compare. Revisions that share
// no history are named separately, because no amount of fetching fixes them.
func ciDiffProblem(event ciEvent, err error) Problem {
	if errors.Is(err, git.ErrNoCommonHistory) {
		return Problem{
			Code:    "ci-no-common-history",
			Message: i18n.Tf("check.problem.ci_no_common_history", event.Name),
			Fix:     i18n.T("check.fix.ci_no_common_history"),
		}
	}
	return Problem{
		Code:    "ci-diff-failed",
		Message: i18n.Tf("check.problem.ci_diff_failed", event.Name, err),
		Fix:     i18n.T("check.fix.ci_diff_failed"),
	}
}

// ciEventRange reads the GitHub event that started the run. An event with a skip
// reason has nothing to compare, and a nil problem means the run is local.
func ciEventRange() (*ciEvent, *Problem) {
	path := strings.TrimSpace(os.Getenv("GITHUB_EVENT_PATH"))
	if path == "" {
		// GITHUB_ACTIONS without an event path is a broken CI run rather than a
		// local one: no event means nothing was checked.
		if strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")) == "true" {
			return nil, ciEventProblem(i18n.T("check.problem.ci_event_no_path"))
		}
		return &ciEvent{Skipped: ciSkipNoEvent}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ciEventProblem(i18n.Tf("check.problem.ci_event_unreadable", path, err))
	}
	return parseCIEvent(os.Getenv("GITHUB_EVENT_NAME"), data)
}

// ciEventProblem is an event the check cannot read, reported with the one fix
// that applies to all of them.
func ciEventProblem(message string) *Problem {
	return &Problem{
		Code:    "ci-event-unusable",
		Message: message,
		Fix:     i18n.T("check.fix.ci_event_unusable"),
	}
}

// parseCIEvent reads the revisions the event carries, selecting the input by the
// event type: a pull request names its base and head, a push names the revisions
// before and after it, and a merge group names the base and head of the queued
// merge. A run without GITHUB_EVENT_NAME has the type inferred from the payload.
func parseCIEvent(name string, data []byte) (*ciEvent, *Problem) {
	var payload struct {
		PullRequest *struct {
			Base struct {
				SHA string `json:"sha"`
			} `json:"base"`
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		MergeGroup *struct {
			BaseSHA string `json:"base_sha"`
			HeadSHA string `json:"head_sha"`
		} `json:"merge_group"`
		Ref    string `json:"ref"`
		Before string `json:"before"`
		After  string `json:"after"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, ciEventProblem(i18n.Tf("check.problem.ci_event_invalid", err))
	}
	event := strings.TrimSpace(name)
	if event == "" {
		switch {
		case payload.PullRequest != nil:
			event = eventPullRequest
		case payload.MergeGroup != nil:
			event = eventMergeGroup
		case payload.Before != "" || payload.After != "":
			event = eventPush
		}
	}
	switch {
	case event == eventPullRequest, event == eventPullRequestTarget:
		// The event name is matched exactly: pull_request_review and the other
		// pull request events carry no pull_request object, and reporting them as
		// a pull request without revisions would misdescribe the run.
		if payload.PullRequest == nil {
			return nil, missingRefsProblem(event, "pull_request")
		}
		base := strings.TrimSpace(payload.PullRequest.Base.SHA)
		head := strings.TrimSpace(payload.PullRequest.Head.SHA)
		if !validSHA(base) || !validSHA(head) {
			return nil, missingRefsProblem(event, "pull_request.base.sha, pull_request.head.sha")
		}
		return &ciEvent{Name: event, Base: base, Head: head}, nil
	case event == eventMergeGroup:
		if payload.MergeGroup == nil {
			return nil, missingRefsProblem(event, "merge_group")
		}
		base := strings.TrimSpace(payload.MergeGroup.BaseSHA)
		head := strings.TrimSpace(payload.MergeGroup.HeadSHA)
		if !validSHA(base) || !validSHA(head) {
			return nil, missingRefsProblem(event, "merge_group.base_sha, merge_group.head_sha")
		}
		return &ciEvent{Name: event, Base: base, Head: head}, nil
	case event == eventPush:
		if strings.HasPrefix(strings.TrimSpace(payload.Ref), "refs/tags/") {
			// A tag push moves no branch content; the branch carrying the tagged
			// commit was already checked when it was pushed.
			return &ciEvent{Name: event, Skipped: ciSkipTagPush}, nil
		}
		before := strings.TrimSpace(payload.Before)
		after := strings.TrimSpace(payload.After)
		// The all-zero revision means "no object": after is a removed branch and
		// before is a branch this push created. Both are legitimate pushes with
		// nothing of their own to judge, and neither is a missing event field.
		if isZeroSHA(after) {
			return &ciEvent{Name: event, Skipped: ciSkipDeletedBranch}, nil
		}
		if !validSHA(after) {
			return nil, missingRefsProblem(event, "after")
		}
		if isZeroSHA(before) {
			// Comparing a new branch against nothing would demand a record for
			// every file the repository already had. What lands is the pull
			// request's business.
			return &ciEvent{Name: event, Skipped: ciSkipNewBranch, Head: after}, nil
		}
		if !validSHA(before) {
			return nil, missingRefsProblem(event, "before")
		}
		return &ciEvent{Name: event, Base: before, Head: after}, nil
	case event == "":
		return nil, ciEventProblem(i18n.T("check.problem.ci_event_unidentified"))
	default:
		return nil, ciEventProblem(i18n.Tf("check.problem.ci_event_unsupported", event))
	}
}

// missingRefsProblem reports an event whose payload lacks the revisions the check
// needs. It never blames a push or a merge group for missing pull request fields:
// those events carry their own revisions.
func missingRefsProblem(event, fields string) *Problem {
	if event == "" {
		event = "unknown"
	}
	return ciEventProblem(i18n.Tf("check.problem.ci_event_missing_refs", event, fields))
}

// validSHA reports whether value looks like a commit hash. It keeps an event
// field from being read as a git option, and it reports a payload that names no
// commit as missing input instead of letting git fail on it.
func validSHA(value string) bool {
	if len(value) < 7 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// isZeroSHA reports whether value is the all-zero revision git uses for "no
// object", which is how a push that created or removed a branch names that side.
func isZeroSHA(value string) bool {
	return value != "" && strings.Trim(value, "0") == ""
}
