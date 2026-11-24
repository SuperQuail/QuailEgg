package release

import (
	"context"
	"errors"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// ledgerSession keeps the release ledger of one release while the publish phase
// runs. Every critical transition is written before the external action it
// describes, and a failed write stops the release.
type ledgerSession struct {
	store    state.LedgerStore
	ledger   *state.Ledger
	entry    *state.LedgerEntry
	identity state.ReleaseIdentity
	token    string
	runID    string
	now      func() time.Time
	source   string
	created  bool
	// kind names where the record lives: remote or local.
	kind string
	// root is the repository root, used for the local mirror of a remote record.
	root string
}

// Identity implements the release progress contract.
func (s *ledgerSession) Identity() state.ReleaseIdentity {
	if s == nil {
		return state.ReleaseIdentity{}
	}
	return s.identity
}

// Outcome implements the release progress contract: it reports what an earlier
// run recorded for one package.
func (s *ledgerSession) Outcome(pkg string) (state.PackageOutcome, string, bool) {
	record := s.packageRecord(pkg)
	if record == nil {
		return state.OutcomeNotStarted, "", false
	}
	return record.Outcome, record.Reason, true
}

// Step implements the release progress contract.
func (s *ledgerSession) Step(name string) (state.StepState, bool) {
	if s == nil || s.entry == nil {
		return state.StepState{}, false
	}
	step, ok := s.entry.Steps[name]
	if !ok {
		return state.StepState{}, false
	}
	return state.StepState{Status: step.Status, At: step.At}, true
}

// round returns the decisions recorded for this release.
func (s *ledgerSession) round() state.RoundDecisions {
	if s == nil || s.entry == nil {
		return state.RoundDecisions{}
	}
	return s.entry.Round
}

// recordRound persists the decisions of this run. A decision already recorded
// wins, so a resume without the flags keeps the original run's choices.
func (s *ledgerSession) recordRound(decisions state.RoundDecisions) error {
	stored := s.entry.Round
	if stored.NoPublish != "" {
		decisions.NoPublish = stored.NoPublish
	}
	decisions.NoPush = decisions.NoPush || stored.NoPush
	decisions.NoGitHubRelease = decisions.NoGitHubRelease || stored.NoGitHubRelease
	decisions.NoAssets = decisions.NoAssets || stored.NoAssets
	if !decisions.ReleaseEnabled {
		decisions.ReleaseEnabled = stored.ReleaseEnabled
	}
	decisions.Store = stored.Store
	if decisions == stored {
		return nil
	}
	s.entry.Round = decisions
	return s.save()
}

// packageRecord returns a copy of the ledger record of one package, or nil when
// the ledger has never seen it.
func (s *ledgerSession) packageRecord(name string) *state.PackageRecord {
	if s == nil || s.entry == nil {
		return nil
	}
	record := s.entry.Packages[name]
	if record == nil {
		return nil
	}
	copied := *record
	return &copied
}

// begin records that an upload is about to happen. The write must succeed before
// the request leaves: an attempt without a record cannot be told apart from one
// that never happened.
func (s *ledgerSession) begin(pkg PackagePlan, target string) error {
	record := s.record(pkg, target)
	record.Outcome = state.OutcomeStarted
	record.Reason = ""
	record.Error = ""
	record.Attempts++
	record.RunID = s.runID
	record.StartedAt = s.now()
	record.FinishedAt = time.Time{}
	return s.save()
}

// finish records the terminal state of one package. A record that already says
// the same thing is left alone: an idempotent resume must not rewrite the ledger
// ref, which would move the record every run.
func (s *ledgerSession) finish(pkg PackagePlan, target string, outcome state.PackageOutcome, reason string) error {
	before := s.packageRecord(pkg.Name)
	record := s.record(pkg, target)
	if before != nil && before.Outcome == outcome && before.Reason == reason && before.Error == "" {
		return nil
	}
	record.Outcome = outcome
	record.Reason = reason
	record.Error = ""
	record.RunID = s.runID
	record.FinishedAt = s.now()
	return s.save()
}

// fail records an upload that returned an error. A command that never started is
// not an attempt: it goes back to "not-started" so a resume may try again, while
// a dispatched request becomes "unknown" and stops the release.
func (s *ledgerSession) fail(pkg PackagePlan, target string, dispatched bool, cause error) error {
	record := s.record(pkg, target)
	if dispatched {
		record.Outcome = state.OutcomeUnknown
	} else {
		record.Outcome = state.OutcomeNotStarted
		record.Reason = state.ReasonNotDispatched
		record.Attempts--
		if record.Attempts < 0 {
			record.Attempts = 0
		}
	}
	record.RunID = s.runID
	record.FinishedAt = s.now()
	if cause != nil {
		record.Error = cause.Error()
	}
	return s.save()
}

// stepStatus returns the recorded status of one step, or "" when the release
// never recorded it.
func (s *ledgerSession) stepStatus(id string) string {
	if s == nil || s.entry == nil {
		return ""
	}
	step, ok := s.entry.Steps[id]
	if !ok {
		return ""
	}
	return step.Status
}

// step records one git or GitHub step. An executor without a ledger (unit tests
// that drive one step in isolation) simply has nothing to record.
func (s *ledgerSession) step(id, status, reason string) error {
	if s == nil {
		return nil
	}
	if s.entry.Steps == nil {
		s.entry.Steps = map[string]*state.StepLedger{}
	}
	current := s.entry.Steps[id]
	if current != nil && current.Status == status && current.Reason == reason {
		return nil
	}
	s.entry.Steps[id] = &state.StepLedger{Status: status, Reason: reason, At: s.now()}
	return s.save()
}

// record returns the mutable ledger record of one package, creating it when the
// ledger has none yet.
func (s *ledgerSession) record(pkg PackagePlan, target string) *state.PackageRecord {
	if s.entry.Packages == nil {
		s.entry.Packages = map[string]*state.PackageRecord{}
	}
	record := s.entry.Packages[pkg.Name]
	if record == nil {
		record = &state.PackageRecord{
			Key: state.PackageKey{
				Repo: s.identity.Repo, Source: s.source, Package: pkg.Name,
				Version: pkg.Next, Target: target,
			},
			Resolver: pkg.Resolver, Path: pkg.Path,
			Outcome: state.OutcomeNotStarted,
		}
		s.entry.Packages[pkg.Name] = record
	}
	record.Key.Version = pkg.Next
	record.Key.Target = target
	record.Resolver = pkg.Resolver
	record.Path = pkg.Path
	return record
}

// save writes the ledger and stops the release when another run got there first.
func (s *ledgerSession) save() error {
	s.entry.UpdatedAt = s.now()
	token, err := s.store.Save(s.ledger, s.token)
	if err != nil {
		if errors.Is(err, state.ErrLedgerConflict) {
			return errors.New(i18n.T("release.err.ledger_conflict"))
		}
		if errors.Is(err, state.ErrLedgerFormat) {
			return errors.New(i18n.Tf("release.err.ledger_format", s.store.Describe()))
		}
		return errors.New(i18n.Tf("release.err.ledger_write", s.store.Describe(), err))
	}
	s.token = token
	if s.kind == ledgerStoreRemote {
		// The remote record is authoritative, but a local copy is what qegg status
		// --offline reads and what a run without push access can still consult. A
		// failed mirror never blocks a release that is recorded remotely.
		_ = state.LocalLedgerStore{Root: s.root}.Mirror(s.ledger)
	}
	return nil
}

// openLedger loads the ledger of this release, creating the entry for a release
// that has none yet. The creation is exclusive: when another run created it
// first, this run stops instead of writing a second set of records.
func (e *executor) openLedger(ctx context.Context, planning bool) (*ledgerSession, error) {
	identity, err := e.releaseIdentity(ctx, planning)
	if err != nil {
		return nil, err
	}
	store, kind, err := e.ledgerStore(ctx, identity)
	if err != nil {
		return nil, err
	}
	ledger, token, err := store.Load()
	if err != nil {
		if errors.Is(err, state.ErrLedgerFormat) {
			return nil, errors.New(i18n.Tf("release.err.ledger_format", store.Describe()))
		}
		return nil, errors.New(i18n.Tf("release.err.ledger_read", store.Describe(), err))
	}
	session := &ledgerSession{
		store: store, ledger: ledger, token: token, identity: identity,
		runID: e.run.RunID, now: e.d.Now, source: identity.SourceCommit, kind: kind, root: e.d.Root,
	}
	entry := ledger.Find(identity)
	migrated := false
	if entry == nil && kind == ledgerStoreRemote {
		// A run that was allowed no external write recorded the release locally.
		// A later run has to read that record, otherwise the decision of the
		// original run would be lost and the package published again.
		local := state.LocalLedgerStore{Root: e.d.Root}
		if localLedger, _, localErr := local.Load(); localErr == nil {
			if localEntry := localLedger.Find(identity); localEntry != nil {
				entry = localEntry
				ledger.Put(entry)
				migrated = true
			}
		}
	}
	sourceFilled := false
	if entry != nil && entry.Identity.SourceCommit == "" && identity.SourceCommit != "" {
		// The entry was written while the release was planned; this run knows the
		// release commit and pins it so the tags land on the right snapshot.
		entry.Identity.SourceCommit = identity.SourceCommit
		sourceFilled = true
	}
	if entry != nil && entry.Identity.SourceCommit != "" && e.d.Options.SourceCommit == "" {
		// The ledger wins over HEAD: a resume that runs after the default branch
		// moved on still tags and publishes the snapshot the release planned.
		identity = identity.WithSource(entry.Identity.SourceCommit)
		session.identity = identity
		session.source = identity.SourceCommit
	}
	if entry == nil {
		entry = &state.LedgerEntry{
			Key:       identity.Key(),
			Identity:  identity,
			RunID:     e.run.RunID,
			CreatedAt: e.d.Now(),
			UpdatedAt: e.d.Now(),
			Packages:  map[string]*state.PackageRecord{},
		}
		if e.freshRelease() {
			// Seeding "not-started" is what makes a first deploy legal for an
			// ecosystem without a registry query: the record itself proves that no
			// request was sent. A legacy release is left unseeded on purpose, so the
			// publish phase cannot mistake it for a fresh one.
			for _, pkg := range e.plan.Packages {
				target := e.packageTarget(pkg)
				entry.Packages[pkg.Name] = &state.PackageRecord{
					Key: state.PackageKey{
						Repo: identity.Repo, Source: identity.SourceCommit, Package: pkg.Name,
						Version: pkg.Next, Target: target,
					},
					Resolver: pkg.Resolver, Path: pkg.Path,
					Outcome: state.OutcomeNotStarted,
				}
			}
		}
		session.created = true
		ledger.Put(entry)
	} else if entry.Identity.SourceCommit == "" && identity.SourceCommit != "" {
		// A record written before the source commit was pinned keeps the commit
		// this run resolved, so the next resume tags the same snapshot.
		entry.Identity.SourceCommit = identity.SourceCommit
	}
	session.entry = entry
	session.created = session.created || migrated || sourceFilled
	if session.created {
		if migrated {
			session.entry.Round.Store = ledgerStoreRemote
		}
		if err := session.save(); err != nil {
			return nil, err
		}
	} else {
		session.token = token
	}
	return session, nil
}

// freshRelease reports whether this run may treat the release as never attempted
// when the ledger has no entry for it.
//
// Two runs qualify: the one that plans the release (it writes the record before it
// does anything else), and the one that publishes a release planned by a qegg that
// writes records (marker schema 2). In the second case this run had to create the
// record, and the planning run may have kept it locally on another machine, so it
// is read as "no earlier attempt" rather than "record lost". A marker below schema
// 2 was written before records existed and proves nothing.
//
// The cost is documented: a release whose record and tag were both deleted is
// indistinguishable from a first publish for an ecosystem that cannot query its
// registry. An existing release tag still blocks a blind deploy.
func (e *executor) freshRelease() bool {
	if e.plan.Resumed {
		// A resume never seeds: the release it publishes was recorded when it was
		// planned, so an empty record is evidence that the record was lost.
		return false
	}
	if e.plan.Phase == PhaseVersion {
		return true
	}
	return e.plan.Marker != nil && e.plan.Marker.SchemaVersion >= state.MarkerSchemaVersion
}

// markerDescribesPlan reports whether the committed marker is the release this
// plan publishes. While the next release is being planned in commit mode the
// previous marker is still on disk, and it must not lend its identity.
func markerDescribesPlan(cfg *config.Config, plan *Plan, marker *state.Marker) bool {
	if plan == nil || marker == nil {
		return false
	}
	fromMarker := markerReleasePlansFor(cfg, marker, configuredNames(cfg), configuredResolver(cfg))
	if len(fromMarker) != len(plan.Releases) {
		return false
	}
	want := map[string]bool{}
	for _, release := range plan.Releases {
		want[release.Tag+"@"+release.Version] = true
	}
	for _, release := range fromMarker {
		if !want[release.Tag+"@"+release.Version] {
			return false
		}
	}
	return true
}

// releaseIdentity builds the identity of this release. The fingerprint always
// comes from the committed marker when there is one, so the identity of a resume
// matches the identity the run that started the release computed.
func (e *executor) releaseIdentity(ctx context.Context, planning bool) (state.ReleaseIdentity, error) {
	repo, err := e.repoIdentity(ctx)
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	identity := state.ReleaseIdentity{
		SchemaVersion: 1,
		Repo:          repo,
		Tags:          releaseTags(e.plan.Releases),
		Version:       e.plan.Version.Next,
		Channel:       e.plan.Channel,
		Mode:          e.d.Cfg.Release.Mode,
		Fingerprint:   e.releaseFingerprint(),
	}
	if markerDescribesPlan(e.d.Cfg, e.plan, e.plan.Marker) {
		// The committed marker is the identity a resume, a repair and qegg status
		// share; a plan recomputed in this process must not invent another one.
		fromMarker, markerErr := markerIdentity(e.d.Cfg, e.plan.Marker, repo, e.d.Cfg.Release.Mode)
		if markerErr != nil {
			return state.ReleaseIdentity{}, markerErr
		}
		identity = fromMarker
	}
	// The pinned source commit: PR-05 passes it in, otherwise the ledger keeps
	// it, otherwise this run publishes the commit it planned from. While a release
	// is only being planned the release commit does not exist yet, so nothing may
	// pin it here.
	source := e.d.Options.SourceCommit
	if planning {
		return identity.WithSource(source), nil
	}
	if source == "" {
		// The source step pinned the release commit before the ledger was opened
		// (a resume locates it from the marker). Only a run that never pinned one
		// falls back to HEAD, which is the commit it is about to publish.
		source = e.identity.SourceCommit
	}
	if source == "" {
		head, headErr := e.d.Git.HeadSHA(ctx)
		if headErr != nil {
			return state.ReleaseIdentity{}, headErr
		}
		source = head
	}
	return identity.WithSource(source), nil
}

// releaseFingerprint prefers the committed marker fingerprint: the publish plan
// recomputed on a resume may differ in details the fingerprint covers.
func (e *executor) releaseFingerprint() string {
	if e.plan != nil && markerDescribesPlan(e.d.Cfg, e.plan, e.plan.Marker) && e.plan.Marker.Fingerprint != "" {
		return e.plan.Marker.Fingerprint
	}
	if e.plan == nil {
		return ""
	}
	return e.plan.Fingerprint
}

// ledgerStore picks where the record of this release lives. A run that performs
// no external write keeps it locally; every other run records it on the origin
// remote so another runner can read it.
func (e *executor) ledgerStore(ctx context.Context, identity state.ReleaseIdentity) (state.LedgerStore, string, error) {
	local := state.LocalLedgerStore{Root: e.d.Root}
	if !e.externalWrites() || e.d.Cfg.Git.Remote == "" {
		return local, ledgerStoreLocal, nil
	}
	url, err := remoteURL(ctx, e.d.Run, e.d.Root, e.d.Cfg.Git.Remote)
	if err != nil || url == "" {
		// A repository without the configured remote keeps the record next to the
		// run records, which is enough for a run that writes nothing outside.
		return local, ledgerStoreLocal, nil
	}
	store, err := NewGitLedgerStore(e.d.Run, e.d.Root, e.d.Cfg.Git.Remote, url, ledgerRefTag(identity))
	if err != nil {
		return nil, "", err
	}
	return store, ledgerStoreRemote, nil
}

// Ledger store kinds recorded in the release entry.
const (
	ledgerStoreRemote = "remote"
	ledgerStoreLocal  = "local"
)

// externalWrites reports whether this run may touch the outside world.
func (e *executor) externalWrites() bool {
	if !e.d.Options.NoPush {
		return true
	}
	if !e.d.Options.NoPublish && anyPublishable(e.plan.Packages) {
		return true
	}
	return !e.d.Options.NoGitHubRelease && e.d.Cfg.Release.GitHubRelease
}

// anyPublishable reports whether any package would be uploaded.
func anyPublishable(pkgs []PackagePlan) bool {
	for _, pkg := range pkgs {
		if pkg.Publish {
			return true
		}
	}
	return false
}

// repoIdentity names the repository in the ledger.
func (e *executor) repoIdentity(ctx context.Context) (string, error) {
	if e.plan == nil {
		return repoName(ctx, e.d, github.Repo{})
	}
	return repoName(ctx, e.d, e.plan.Repo)
}

// releaseTags lists the tags of the release units.
func releaseTags(releases []ReleasePlan) []string {
	out := make([]string, 0, len(releases))
	for _, release := range releases {
		if release.Tag != "" {
			out = append(out, release.Tag)
		}
	}
	return state.ReleaseSet(out)
}

// ledgerRefTag picks the tag that names the ledger ref of this release.
func ledgerRefTag(identity state.ReleaseIdentity) string {
	if len(identity.Tags) == 0 {
		return "default"
	}
	return identity.Tags[0]
}

// reasonText renders a recorded reason token in the user's language.
func reasonText(reason string) string {
	if reason == "" {
		return ""
	}
	return i18n.T("release.reason." + reason)
}
