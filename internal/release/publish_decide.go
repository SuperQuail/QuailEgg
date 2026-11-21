package release

import (
	"errors"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// packageAction is what the publish phase does with one package.
type packageAction int

const (
	// actionSkip means the package needs no external write.
	actionSkip packageAction = iota
	// actionUpload means the package has to be built and published.
	actionUpload
	// actionDefer means the publication is the release tag, so the tag step
	// completes it.
	actionDefer
	// actionStop means the release cannot continue safely.
	actionStop
)

// packageDecision is the outcome of the per-package rules.
type packageDecision struct {
	action packageAction
	reason string
	// record reports that the decision changes the ledger and has to be written.
	record bool
	// detail explains a stop in the user's language.
	detail string
}

// packageFacts is everything the publish decision may rely on. A package whose
// outcome is unknown never becomes an upload: the release stops instead.
type packageFacts struct {
	// record is the ledger record of the package, nil when the ledger has none
	// (a release started by an older qegg, or a package added later).
	record *state.PackageRecord
	// class is the classified registry answer. It is not consulted for a
	// tag-published package: the release tag is the evidence there.
	class    registry.PrecheckClass
	queryErr error
	// tagPublished reports that publishing the package IS pushing its tag.
	tagPublished bool
	// tag is the release tag that publishes this package.
	tag string
	// remoteTag is the remote state of tag; remoteTagRead reports whether it was
	// actually read.
	remoteTag     remoteTag
	remoteTagRead bool
	// tagMatches reports that the remote tag resolves to the release source.
	tagMatches bool
	// ledgerAware reports that the release was planned by a qegg that records its
	// publish progress, so a missing record means the record was lost.
	ledgerAware bool
}

// decidePackage turns the recorded attempt and the registry answer into the next
// action. The rules are deliberately asymmetric: a confirmed absence allows a
// retry, an unknown outcome never does.
func decidePackage(f packageFacts) packageDecision {
	if f.record != nil {
		switch f.record.Outcome {
		case state.OutcomeUploaded:
			return packageDecision{action: actionSkip, reason: state.ReasonPublished}
		case state.OutcomeSkipped:
			return packageDecision{action: actionSkip, reason: f.record.Reason}
		case state.OutcomeStarted:
			return decideAttempted(f)
		case state.OutcomeUnknown:
			return decideRetry(f)
		}
	}
	return decideFirstAttempt(f)
}

// decideTagged covers a package whose publication is its release tag: the remote
// tag at the release source is the proof that the release happened.
func decideTagged(f packageFacts) packageDecision {
	if !f.remoteTagRead {
		return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.T("release.stop.tag_remote")}
	}
	if f.tagMatches {
		return packageDecision{action: actionSkip, reason: state.ReasonTagPublished, record: true}
	}
	if !f.remoteTag.Present {
		return packageDecision{action: actionDefer, reason: state.ReasonTagPublished, record: true}
	}
	return packageDecision{
		action: actionStop, reason: state.ReasonConflict,
		detail: i18n.Tf("release.stop.tag_conflict", f.tag, shortSHA(f.remoteTag.Commit)),
	}
}

// decideAttempted handles a package whose upload started and left no result.
func decideAttempted(f packageFacts) packageDecision {
	if f.tagPublished {
		return decideTagged(f)
	}
	switch f.class {
	case registry.PrecheckExists:
		return packageDecision{action: actionSkip, reason: state.ReasonExists, record: true}
	case registry.PrecheckAbsent:
		return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.T("release.stop.attempted_absent")}
	case registry.PrecheckFailed:
		return packageDecision{action: actionStop, reason: state.ReasonQueryFailed, detail: queryDetail(f)}
	default:
		return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.T("release.stop.attempted_unknown")}
	}
}

// decideRetry handles a package whose upload returned an error.
func decideRetry(f packageFacts) packageDecision {
	if f.tagPublished {
		return decideTagged(f)
	}
	switch f.class {
	case registry.PrecheckExists:
		return packageDecision{action: actionSkip, reason: state.ReasonExists, record: true}
	case registry.PrecheckAbsent:
		return packageDecision{action: actionUpload, reason: state.ReasonQueryAbsent}
	case registry.PrecheckFailed:
		return packageDecision{action: actionStop, reason: state.ReasonQueryFailed, detail: queryDetail(f)}
	default:
		return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.T("release.stop.retry_unknown")}
	}
}

// decideFirstAttempt handles a package the ledger recorded as pending, or one it
// has never seen at all. The second case is the legacy path: without a record the
// ecosystem has to be able to answer the query.
func decideFirstAttempt(f packageFacts) packageDecision {
	if f.tagPublished {
		return decideTagged(f)
	}
	switch f.class {
	case registry.PrecheckExists:
		return packageDecision{action: actionSkip, reason: state.ReasonExists, record: true}
	case registry.PrecheckAbsent:
		return packageDecision{action: actionUpload, reason: state.ReasonQueryAbsent}
	case registry.PrecheckFailed:
		return packageDecision{action: actionStop, reason: state.ReasonQueryFailed, detail: queryDetail(f)}
	default:
		// No query capability: the recorded attempt and the missing tag are the
		// only evidence. An already published tag means the release finished, so
		// an unrecorded upload stays unproven.
		if f.remoteTagRead && f.remoteTag.Present {
			return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.Tf("release.stop.unrecorded_tag", f.tag)}
		}
		if f.record == nil {
			if f.ledgerAware {
				return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.T("release.stop.record_lost")}
			}
			return packageDecision{action: actionStop, reason: state.ReasonUnverified, detail: i18n.T("release.stop.legacy_unknown")}
		}
		return packageDecision{action: actionUpload, reason: state.ReasonUnverified}
	}
}

// queryDetail returns the readable reason a query failed.
func queryDetail(f packageFacts) string {
	if f.queryErr != nil {
		return f.queryErr.Error()
	}
	return i18n.T("release.stop.query_failed")
}

// decisionError renders a stop decision as the error the release exits with. It
// names the package, the version, the target, the last recorded action and what
// to check before publishing again.
func decisionError(pkg PackagePlan, target string, f packageFacts, decision packageDecision) error {
	last := i18n.T("release.action.none")
	if f.record != nil {
		last = recordActionText(f.record)
	}
	detail := decision.detail
	if detail == "" {
		detail = reasonText(decision.reason)
	}
	return errors.New(i18n.Tf("release.err.publish_stop", pkg.Name, pkg.Next, targetLabel(target), last, detail))
}

// recordActionText names the last recorded action of one package.
func recordActionText(rec *state.PackageRecord) string {
	if rec == nil {
		return i18n.T("release.action.none")
	}
	action := i18n.T("release.action." + strings.ReplaceAll(string(rec.Outcome), "-", "_"))
	if rec.Attempts > 0 {
		action = i18n.Tf("release.action.attempted", action, rec.Attempts)
	}
	return action
}

// targetLabel keeps the message readable when an ecosystem declares no target.
func targetLabel(target string) string {
	if target == "" {
		return i18n.T("release.action.unknown_target")
	}
	return target
}
