package release

import (
	"errors"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// TestDecidePackageMatrix pins the rule that a resume never uploads on a guess:
// only a confirmed absence or a recorded "never attempted" state may publish.
func TestDecidePackageMatrix(t *testing.T) {
	tag := remoteTag{Present: true, Commit: "aaaa"}
	cases := []struct {
		name       string
		facts      packageFacts
		want       packageAction
		wantReason string
		wantRecord bool
	}{
		{
			name:       "first attempt with a confirmed absence uploads",
			facts:      packageFacts{record: pending(), class: registry.PrecheckAbsent},
			want:       actionUpload,
			wantReason: state.ReasonQueryAbsent,
		},
		{
			name:       "first attempt with an existing version is skipped",
			facts:      packageFacts{record: pending(), class: registry.PrecheckExists},
			want:       actionSkip,
			wantReason: state.ReasonExists,
			wantRecord: true,
		},
		{
			name:  "a failed query stops the release",
			facts: packageFacts{record: pending(), class: registry.PrecheckFailed, queryErr: errors.New("401 Unauthorized")},
			want:  actionStop,
		},
		{
			name:  "a legacy release without a query capability stops",
			facts: packageFacts{class: registry.PrecheckUnknown},
			want:  actionStop,
		},
		{
			name:  "a recorded first deploy runs without a query capability",
			facts: packageFacts{record: pending(), class: registry.PrecheckUnknown},
			want:  actionUpload,
		},
		{
			name:       "an upload whose result is unknown is skipped when the registry has the version",
			facts:      packageFacts{record: started(), class: registry.PrecheckExists},
			want:       actionSkip,
			wantReason: state.ReasonExists,
			wantRecord: true,
		},
		{
			name:  "an upload whose result is unknown never retries on a missing answer",
			facts: packageFacts{record: started(), class: registry.PrecheckAbsent},
			want:  actionStop,
		},
		{
			name:  "an upload whose result is unknown stops when the registry cannot answer",
			facts: packageFacts{record: started(), class: registry.PrecheckUnknown},
			want:  actionStop,
		},
		{
			name:  "an upload whose result is unknown stops when the query fails",
			facts: packageFacts{record: started(), class: registry.PrecheckFailed, queryErr: errors.New("timeout")},
			want:  actionStop,
		},
		{
			name:       "a failed upload retries once the registry confirms the absence",
			facts:      packageFacts{record: unknown(), class: registry.PrecheckAbsent},
			want:       actionUpload,
			wantReason: state.ReasonQueryAbsent,
		},
		{
			name:  "a failed upload with no query capability stops",
			facts: packageFacts{record: unknown(), class: registry.PrecheckUnknown},
			want:  actionStop,
		},
		{
			name:  "a finished package needs no upload",
			facts: packageFacts{record: uploaded(), class: registry.PrecheckAbsent},
			want:  actionSkip,
		},
		{
			name:       "a skipped package keeps its reason",
			facts:      packageFacts{record: skipped(state.ReasonPublishFalse), class: registry.PrecheckAbsent},
			want:       actionSkip,
			wantReason: state.ReasonPublishFalse,
		},
		{
			name:       "a tag-published package waits for its tag",
			facts:      packageFacts{record: pending(), tagPublished: true, remoteTagRead: true},
			want:       actionDefer,
			wantReason: state.ReasonTagPublished,
			wantRecord: true,
		},
		{
			name:       "a tag-published package is done once the remote tag matches",
			facts:      packageFacts{record: pending(), tagPublished: true, remoteTagRead: true, remoteTag: tag, tagMatches: true},
			want:       actionSkip,
			wantReason: state.ReasonTagPublished,
			wantRecord: true,
		},
		{
			name: "a tag-published package stops when the remote tag points elsewhere",
			facts: packageFacts{
				record: started(), tagPublished: true, remoteTagRead: true,
				remoteTag: remoteTag{Present: true, Commit: "bbbb"},
			},
			want: actionStop,
		},
		{
			name:  "a tag-published package stops when the remote tag cannot be read",
			facts: packageFacts{record: pending(), tagPublished: true},
			want:  actionStop,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decidePackage(tc.facts)
			if got.action != tc.want {
				t.Fatalf("action = %v, want %v (reason %q)", got.action, tc.want, got.reason)
			}
			if tc.wantReason != "" && got.reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got.reason, tc.wantReason)
			}
			if got.record != tc.wantRecord {
				t.Fatalf("record = %v, want %v", got.record, tc.wantRecord)
			}
			if got.action == actionStop && got.detail == "" {
				t.Fatal("a stop needs a detail explaining what to check")
			}
		})
	}
}

func pending() *state.PackageRecord {
	return &state.PackageRecord{Outcome: state.OutcomeNotStarted}
}

func started() *state.PackageRecord {
	return &state.PackageRecord{Outcome: state.OutcomeStarted, Attempts: 1}
}

func unknown() *state.PackageRecord {
	return &state.PackageRecord{Outcome: state.OutcomeUnknown, Attempts: 1, Error: "exit status 1"}
}

func uploaded() *state.PackageRecord {
	return &state.PackageRecord{Outcome: state.OutcomeUploaded}
}

func skipped(reason string) *state.PackageRecord {
	return &state.PackageRecord{Outcome: state.OutcomeSkipped, Reason: reason}
}
