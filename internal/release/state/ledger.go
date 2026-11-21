package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// LedgerFileName is the local copy of the release ledger. It lives next to the
// run records, inside the gitignored state directory.
const LedgerFileName = "ledger.json"

// LedgerSchemaVersion is the ledger format this qegg writes.
const LedgerSchemaVersion = 1

// PackageOutcome is the recorded state of one package publication. The set is
// frozen so two runs of different qegg versions agree on what a record means.
type PackageOutcome string

// Package outcomes.
const (
	// OutcomeNotStarted means the release record exists and no request was sent.
	OutcomeNotStarted PackageOutcome = "not-started"
	// OutcomeStarted means a request may be in flight; the result is unknown.
	OutcomeStarted PackageOutcome = "started"
	// OutcomeUploaded means the target confirmed the publication.
	OutcomeUploaded PackageOutcome = "uploaded"
	// OutcomeSkipped means the package was deliberately not published.
	OutcomeSkipped PackageOutcome = "skipped"
	// OutcomeUnknown means a request finished with an error that may or may not
	// have landed.
	OutcomeUnknown PackageOutcome = "unknown"
)

// PackageKey identifies one package publication: repository, release source
// commit, package, version and target registry.
type PackageKey struct {
	Repo    string `json:"repo"`
	Source  string `json:"source,omitempty"`
	Package string `json:"package"`
	Version string `json:"version"`
	Target  string `json:"target,omitempty"`
}

// Reason tokens. They are stable machine identifiers; the user-facing text is
// resolved through the message catalog under release.reason.*.
const (
	ReasonNoPublish     = "no_publish"
	ReasonPublishFalse  = "publish_false"
	ReasonNoAssets      = "no_assets"
	ReasonNoPush        = "no_push"
	ReasonNoRelease     = "no_release"
	ReasonReleaseOff    = "release_off"
	ReasonExists        = "already_published"
	ReasonPublished     = "published"
	ReasonTagPublished  = "tag_published"
	ReasonQueryFailed   = "query_failed"
	ReasonQueryAbsent   = "query_absent"
	ReasonUnverified    = "unverified"
	ReasonConflict      = "conflict"
	ReasonTagMismatch   = "tag_mismatch"
	ReasonNotDispatched = "not_dispatched"
)

// ErrLedgerConflict reports that another writer changed the ledger after this
// run read it. The caller stops instead of repeating an external write.
var ErrLedgerConflict = fmt.Errorf("release ledger was changed by another run")

// ErrLedgerFormat reports a ledger this qegg cannot read. The caller renders it
// in the user's language instead of printing the underlying technical detail.
var ErrLedgerFormat = fmt.Errorf("release ledger format is not supported")

// PackageRecord is the execution record of one package.
type PackageRecord struct {
	Key        PackageKey     `json:"key"`
	Resolver   string         `json:"resolver,omitempty"`
	Path       string         `json:"path,omitempty"`
	Outcome    PackageOutcome `json:"outcome"`
	Reason     string         `json:"reason,omitempty"`
	Query      string         `json:"query,omitempty"`
	Attempts   int            `json:"attempts,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	Error      string         `json:"error,omitempty"`
	StartedAt  time.Time      `json:"started_at,omitempty"`
	FinishedAt time.Time      `json:"finished_at,omitempty"`
}

// StepLedger is the execution record of one git or GitHub step.
type StepLedger struct {
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitempty"`
}

// RoundDecisions keeps the decisions the first run made for the whole release,
// so a later resume keeps them even when the flags are not repeated.
type RoundDecisions struct {
	NoPublish       string `json:"no_publish,omitempty"`
	NoPush          bool   `json:"no_push,omitempty"`
	NoGitHubRelease bool   `json:"no_github_release,omitempty"`
	NoAssets        bool   `json:"no_assets,omitempty"`
	// ReleaseEnabled records whether the original run wanted a GitHub Release.
	ReleaseEnabled bool `json:"release_enabled"`
	// Store names where this entry lives: remote or local.
	Store string `json:"store,omitempty"`
}

// LedgerEntry is one release: the frozen release identity plus what happened to
// each of its packages and steps.
type LedgerEntry struct {
	Key       string                    `json:"key"`
	Identity  ReleaseIdentity           `json:"identity"`
	RunID     string                    `json:"run_id,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
	Round     RoundDecisions            `json:"round"`
	Packages  map[string]*PackageRecord `json:"packages,omitempty"`
	Steps     map[string]*StepLedger    `json:"steps,omitempty"`
}

// Ledger is every release qegg has recorded in one place, keyed by the identity
// key of its release.
type Ledger struct {
	SchemaVersion int                     `json:"schema_version"`
	Entries       map[string]*LedgerEntry `json:"entries"`
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{SchemaVersion: LedgerSchemaVersion, Entries: map[string]*LedgerEntry{}}
}

// ParseLedger decodes a ledger and rejects a format this qegg cannot read.
func ParseLedger(data []byte) (*Ledger, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return NewLedger(), nil
	}
	var ledger Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLedgerFormat, err)
	}
	if ledger.SchemaVersion == 0 {
		ledger.SchemaVersion = LedgerSchemaVersion
	}
	if ledger.SchemaVersion != LedgerSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version %d, this qegg reads %d", ErrLedgerFormat, ledger.SchemaVersion, LedgerSchemaVersion)
	}
	if ledger.Entries == nil {
		ledger.Entries = map[string]*LedgerEntry{}
	}
	for key, entry := range ledger.Entries {
		if entry == nil {
			delete(ledger.Entries, key)
			continue
		}
		if entry.Packages == nil {
			entry.Packages = map[string]*PackageRecord{}
		}
		if entry.Key == "" {
			entry.Key = entry.Identity.Key()
		}
	}
	return &ledger, nil
}

// Marshal renders the ledger with a stable tag order so two runs that recorded
// the same state produce the same bytes.
func (l *Ledger) Marshal() ([]byte, error) {
	if l.SchemaVersion == 0 {
		l.SchemaVersion = LedgerSchemaVersion
	}
	if l.Entries == nil {
		l.Entries = map[string]*LedgerEntry{}
	}
	keys := make([]string, 0, len(l.Entries))
	for key := range l.Entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]*LedgerEntry, len(keys))
	for _, key := range keys {
		entry := l.Entries[key]
		sort.Strings(entry.Identity.Tags)
		ordered[key] = entry
	}
	l.Entries = ordered
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Find returns the entry of one release. The identity key wins; a scan with
// SameRelease catches a marker whose fingerprint or channel is unknown.
func (l *Ledger) Find(id ReleaseIdentity) *LedgerEntry {
	if l == nil {
		return nil
	}
	if entry, ok := l.Entries[id.Key()]; ok {
		return entry
	}
	best := (*LedgerEntry)(nil)
	for _, entry := range l.Entries {
		if !entry.Identity.SameRelease(id) {
			continue
		}
		if best == nil || entry.UpdatedAt.After(best.UpdatedAt) {
			best = entry
		}
	}
	return best
}

// Put stores an entry.
func (l *Ledger) Put(entry *LedgerEntry) {
	if l.Entries == nil {
		l.Entries = map[string]*LedgerEntry{}
	}
	if entry.Key == "" {
		entry.Key = entry.Identity.Key()
	}
	l.Entries[entry.Key] = entry
}

// ReleaseSet renders a set of release tags in a stable order.
func ReleaseSet(tags []string) []string {
	out := append([]string(nil), tags...)
	sort.Strings(out)
	return out
}

// LedgerPath returns the local ledger path for a repository root.
func LedgerPath(root string) string {
	return filepath.Join(StateDir(root), LedgerFileName)
}

// LedgerStore persists the ledger so a resume on another runner can read what an
// earlier run already attempted. Load returns an opaque revision token that Save
// hands back: Save fails with ErrLedgerConflict when another writer got there
// first, which is what keeps two concurrent resumes from repeating an upload.
type LedgerStore interface {
	Load() (*Ledger, string, error)
	Save(ledger *Ledger, token string) (string, error)
	// Describe names the store for user-facing messages.
	Describe() string
}

// LocalLedgerStore keeps the ledger in the gitignored state directory. It is
// used by a run that performs no external write, and by tests.
type LocalLedgerStore struct {
	Root string
}

// Describe implements LedgerStore.
func (s LocalLedgerStore) Describe() string { return LedgerPath(s.Root) }

// Load implements LedgerStore.
func (s LocalLedgerStore) Load() (*Ledger, string, error) {
	data, err := os.ReadFile(LedgerPath(s.Root))
	if err != nil {
		if os.IsNotExist(err) {
			return NewLedger(), "", nil
		}
		return nil, "", err
	}
	ledger, err := ParseLedger(data)
	if err != nil {
		return nil, "", err
	}
	return ledger, digest(data), nil
}

// Save implements LedgerStore.
func (s LocalLedgerStore) Save(ledger *Ledger, token string) (string, error) {
	dir := StateDir(s.Root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	lock := flock.New(filepath.Join(s.Root, metadataDirName, "lock"))
	if err := lock.Lock(); err != nil {
		return "", err
	}
	defer lock.Unlock()

	data, err := ledger.Marshal()
	if err != nil {
		return "", err
	}
	current := ""
	if existing, readErr := os.ReadFile(LedgerPath(s.Root)); readErr == nil {
		current = digest(existing)
	} else if !os.IsNotExist(readErr) {
		return "", readErr
	}
	if current != token {
		return "", ErrLedgerConflict
	}
	path := LedgerPath(s.Root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return digest(data), nil
}

// Mirror writes the ledger without a compare-and-swap. It is the local copy of a
// record whose authority is the remote, kept so qegg status --offline and an
// interrupted run can still read what the release did.
func (s LocalLedgerStore) Mirror(ledger *Ledger) error {
	dir := StateDir(s.Root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(s.Root, metadataDirName, "lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()
	data, err := ledger.Marshal()
	if err != nil {
		return err
	}
	path := LedgerPath(s.Root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
