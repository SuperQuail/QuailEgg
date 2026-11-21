package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// sourcesDirName is the directory holding the recorded release snapshots.
const sourcesDirName = "sources"

// Origins of a recorded release source commit.
const (
	// SourceOriginCommit is the commit the version phase created; qegg recorded
	// it right after git returned the hash.
	SourceOriginCommit = "commit"
	// SourceOriginHistory is a commit located in the branch history.
	SourceOriginHistory = "history"
	// SourceOriginPinned is a commit the caller supplied with Options.
	SourceOriginPinned = "pinned"
)

// SourceRecord pins the commit a release is built, uploaded and tagged from.
//
// The version phase writes the marker and only then creates the commit that
// carries it, so a marker can never name its own commit. This record is written
// as soon as git returned the hash and is keyed by ReleaseIdentity.Key(), so a
// later run that sees the same release intent finds the same snapshot even when
// the branch has moved on. It lives beside the run records under
// .quailegg/state, which git ignores: it shortens a resume on the same machine,
// while the branch history stays the fallback that also works from a fresh
// runner.
type SourceRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Key           string          `json:"key"`
	Identity      ReleaseIdentity `json:"identity"`
	Commit        string          `json:"commit"`
	Origin        string          `json:"origin,omitempty"`
	RecordedAt    time.Time       `json:"recorded_at"`
}

// SourcePath returns the record path of a release identity.
func SourcePath(root, key string) string {
	return filepath.Join(StateDir(root), sourcesDirName, key+".json")
}

// LoadSource reads the recorded source commit; a missing record returns
// (nil, nil).
func LoadSource(root, key string) (*SourceRecord, error) {
	if key == "" {
		return nil, nil
	}
	data, err := fsx.ReadOrEmpty(SourcePath(root, key))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var record SourceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("%s: %w", SourcePath(root, key), err)
	}
	return &record, nil
}

// SaveSource writes the record atomically.
func SaveSource(root string, record *SourceRecord) error {
	if record == nil || record.Commit == "" {
		return nil
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = 1
	}
	if record.Key == "" {
		record.Key = record.Identity.Key()
	}
	if record.Key == "" {
		return nil
	}
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	path := SourcePath(root, record.Key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, append(data, '\n'), 0o644)
}
