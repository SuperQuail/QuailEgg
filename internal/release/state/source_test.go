package state

import (
	"testing"
	"time"
)

func TestReleaseIdentityKeyIgnoresSourceCommit(t *testing.T) {
	id := ReleaseIdentity{
		SchemaVersion: 1, Repo: "example/demo", Tags: []string{"v1.1.0", "demo@1.1.0"},
		Version: "1.1.0", Mode: "pr", Fingerprint: "sha256:abc",
	}
	other := id
	other.Tags = []string{"demo@1.1.0", "v1.1.0"}
	if id.Key() != other.Key() {
		t.Fatalf("the key must not depend on tag order: %q vs %q", id.Key(), other.Key())
	}
	if len(id.Key()) != 32 {
		t.Fatalf("key length = %d", len(id.Key()))
	}
	pinned := id.WithSource("0123456789abcdef0123456789abcdef01234567")
	if pinned.Key() != id.Key() {
		t.Fatalf("pinning the source must keep the identity key: %q vs %q", pinned.Key(), id.Key())
	}
	if pinned.SourceCommit == "" {
		t.Fatal("WithSource must record the commit")
	}
}

func TestReleaseIdentitySameRelease(t *testing.T) {
	id := ReleaseIdentity{Repo: "example/demo", Version: "1.1.0", Mode: "pr", Fingerprint: "sha256:abc", Tags: []string{"v1.1.0"}}
	if !id.SameRelease(ReleaseIdentity{Fingerprint: "sha256:abc"}) {
		t.Fatal("an empty field means unknown and must match")
	}
	if id.SameRelease(ReleaseIdentity{Repo: "example/demo", Version: "1.2.0", Fingerprint: "sha256:abc"}) {
		t.Fatal("a different version is a different release")
	}
	if id.SameRelease(ReleaseIdentity{Repo: "other/demo", Version: "1.1.0", Mode: "pr", Fingerprint: "sha256:abc"}) {
		t.Fatal("a different repository is a different release")
	}
	pinned := id.WithSource("0123456789abcdef0123456789abcdef01234567")
	if !id.SameRelease(pinned) {
		t.Fatal("the source commit must not affect the release identity")
	}
}

func TestSaveAndLoadSourceRecord(t *testing.T) {
	root := t.TempDir()
	id := ReleaseIdentity{SchemaVersion: 1, Repo: "example/demo", Tags: []string{"v1.1.0"}, Version: "1.1.0", Mode: "pr", Fingerprint: "sha256:abc"}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	record := &SourceRecord{Identity: id.WithSource("abc1234"), Commit: "abc1234", Origin: SourceOriginHistory, RecordedAt: at}
	if err := SaveSource(root, record); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if record.Key != id.Key() {
		t.Fatalf("record key = %q, want the identity key %q", record.Key, id.Key())
	}
	loaded, err := LoadSource(root, id.Key())
	if err != nil || loaded == nil {
		t.Fatalf("LoadSource = %+v, %v", loaded, err)
	}
	if loaded.Commit != "abc1234" || loaded.Identity.Fingerprint != id.Fingerprint || loaded.Origin != SourceOriginHistory {
		t.Fatalf("unexpected record: %+v", loaded)
	}
	missing, err := LoadSource(root, "0123456789abcdef0123456789abcdef")
	if err != nil || missing != nil {
		t.Fatalf("a missing record must return nil, nil; got %+v, %v", missing, err)
	}
}
