package state

import (
	"testing"
)

func TestIdentityKeyExcludesSourceCommit(t *testing.T) {
	id := ReleaseIdentity{Repo: "github.com/o/r", Tags: []string{"v1.2.0"}, Version: "1.2.0", Mode: "pr", Fingerprint: "sha256:abc"}
	moved := id.WithSource("bbbb")
	pinned := id.WithSource("aaaa")
	if moved.Key() != pinned.Key() {
		t.Fatalf("the source commit must not change the release key: %s vs %s", moved.Key(), pinned.Key())
	}
	if moved.Key() == "" {
		t.Fatal("empty release key")
	}
	other := id
	other.Version = "1.3.0"
	if other.Key() == id.Key() {
		t.Fatal("another version must change the release key")
	}
	tags := id
	tags.Tags = []string{"v1.3.0"}
	if tags.Key() == id.Key() {
		t.Fatal("another tag must change the release key")
	}
}

func TestSameReleaseTreatsEmptyAsUnknown(t *testing.T) {
	full := ReleaseIdentity{Repo: "github.com/o/r", Tags: []string{"v1.0.0"}, Version: "1.0.0", Mode: "commit", Fingerprint: "sha256:abc"}
	legacy := ReleaseIdentity{Repo: "github.com/o/r", Tags: []string{"v1.0.0"}, Version: "1.0.0"}
	if !full.SameRelease(legacy) || !legacy.SameRelease(full) {
		t.Fatal("an unknown fingerprint must not make two identities different")
	}
	other := legacy
	other.Version = "2.0.0"
	if full.SameRelease(other) {
		t.Fatal("different versions are different releases")
	}
	repeated := full
	repeated.Tags = []string{"v1.0.0", "v1.0.0"}
	if full.Key() != repeated.Key() || !full.SameRelease(repeated) {
		t.Fatal("a repeated tag is the same release")
	}
}

func TestParseLedgerRejectsUnknownSchema(t *testing.T) {
	if _, err := ParseLedger([]byte(`{"schema_version": 99}`)); err == nil {
		t.Fatal("an unknown schema_version must be rejected")
	}
	if _, err := ParseLedger(nil); err != nil {
		t.Fatalf("an empty ledger is fine: %v", err)
	}
	ledger, err := ParseLedger([]byte(`{"entries": {"k": {"packages": {"a": {"outcome": "uploaded"}}}}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ledger.SchemaVersion != LedgerSchemaVersion {
		t.Fatalf("schema version was not defaulted: %d", ledger.SchemaVersion)
	}
	if got := ledger.Entries["k"].Packages["a"].Outcome; got != OutcomeUploaded {
		t.Fatalf("outcome = %q", got)
	}
}

func TestLedgerMarshalIsStable(t *testing.T) {
	ledger := NewLedger()
	ledger.Put(&LedgerEntry{
		Identity: ReleaseIdentity{Repo: "r", Tags: []string{"v2", "v1"}, Version: "1.0.0"},
		Packages: map[string]*PackageRecord{"b": {Outcome: OutcomeUploaded}, "a": {Outcome: OutcomeSkipped}},
	})
	first, err := ledger.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("marshal is not stable:\n%s\n%s", first, second)
	}
	for key, stored := range ledger.Entries {
		if key != stored.Identity.Key() {
			t.Fatalf("entry key %q does not match its identity %q", key, stored.Identity.Key())
		}
	}
}

func TestLocalLedgerStoreCompareAndSwap(t *testing.T) {
	root := t.TempDir()
	store := LocalLedgerStore{Root: root}
	ledger, token, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if token != "" || len(ledger.Entries) != 0 {
		t.Fatalf("a missing ledger must load empty: %q", token)
	}
	entry := &LedgerEntry{Identity: ReleaseIdentity{Repo: "r", Version: "1.0.0"}, Packages: map[string]*PackageRecord{}}
	ledger.Put(entry)
	first, err := store.Save(ledger, token)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if first == "" {
		t.Fatal("save must return the new revision")
	}

	// Two runs that read the same revision: the second write loses.
	stale, staleToken, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if staleToken != first {
		t.Fatalf("token = %q, want %q", staleToken, first)
	}
	entry.Packages["a"] = &PackageRecord{Outcome: OutcomeStarted}
	if _, err := store.Save(ledger, first); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if _, err := store.Save(stale, staleToken); err == nil {
		t.Fatal("a stale write must be rejected")
	} else if err != ErrLedgerConflict {
		t.Fatalf("error = %v, want ErrLedgerConflict", err)
	}

	reloaded, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Entries[entry.Key].Packages["a"].Outcome; got != OutcomeStarted {
		t.Fatalf("the winning write was lost: %q", got)
	}
}

func TestLedgerFindFallsBackToSameRelease(t *testing.T) {
	ledger := NewLedger()
	identity := ReleaseIdentity{Repo: "r", Tags: []string{"v1.0.0"}, Version: "1.0.0", Mode: "commit", Fingerprint: "sha256:abc"}
	ledger.Put(&LedgerEntry{Identity: identity})
	if found := ledger.Find(identity); found == nil {
		t.Fatal("exact key lookup failed")
	}
	// A legacy marker without a fingerprint must still find the record.
	legacy := ReleaseIdentity{Repo: "r", Tags: []string{"v1.0.0"}, Version: "1.0.0", Mode: "commit"}
	if found := ledger.Find(legacy); found == nil {
		t.Fatal("SameRelease lookup failed")
	}
	other := identity
	other.Version = "9.9.9"
	if found := ledger.Find(other); found != nil {
		t.Fatal("another version must not match")
	}
}
