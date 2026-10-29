package state

import (
	"strings"
	"testing"
)

func TestPreReleaseRoundTrip(t *testing.T) {
	root := t.TempDir()
	if pre, err := LoadPreRelease(root); err != nil || pre != nil {
		t.Fatalf("missing lock = (%v, %v), want (nil, nil)", pre, err)
	}
	want := &PreRelease{Channel: "alpha", Version: "0.5.0-alpha.2"}
	if err := SavePreRelease(root, want); err != nil {
		t.Fatalf("SavePreRelease: %v", err)
	}
	got, err := LoadPreRelease(root)
	if err != nil {
		t.Fatalf("LoadPreRelease: %v", err)
	}
	if got.Channel != want.Channel || got.Version != want.Version || got.SchemaVersion != 1 {
		t.Fatalf("loaded %+v", got)
	}
	if err := RemovePreRelease(root); err != nil {
		t.Fatalf("RemovePreRelease: %v", err)
	}
	if err := RemovePreRelease(root); err != nil {
		t.Fatalf("second RemovePreRelease: %v", err)
	}
	if pre, _ := LoadPreRelease(root); pre != nil {
		t.Fatal("lock survived removal")
	}
}

func TestPreReleaseValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "channel only", body: `{"schema_version":1,"channel":"beta"}`, ok: true},
		{name: "with version", body: `{"schema_version":1,"channel":"rc","version":"1.2.3-rc.4"}`, ok: true},
		{name: "unknown field", body: `{"schema_version":1,"channel":"alpha","extra":true}`, ok: true},
		{name: "bad channel", body: `{"schema_version":1,"channel":"nightly"}`},
		{name: "channel mismatch", body: `{"schema_version":1,"channel":"alpha","version":"1.2.3-beta.1"}`},
		{name: "bad schema", body: `{"schema_version":2,"channel":"alpha"}`},
		{name: "not json", body: `nope`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePreRelease([]byte(tc.body), "pre-release.lock")
			if tc.ok && err != nil {
				t.Fatalf("ParsePreRelease: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestPreReleaseErrorMentionsPath(t *testing.T) {
	_, err := ParsePreRelease([]byte(`{"schema_version":1,"channel":"nope"}`), ".quailegg/pre-release.lock")
	if err == nil || !strings.Contains(err.Error(), ".quailegg/pre-release.lock") {
		t.Fatalf("error %v should mention the lock path", err)
	}
}
