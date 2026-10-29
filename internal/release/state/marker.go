package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// metadataDirName is the directory holding release metadata.
const metadataDirName = ".quailegg"

// releaseFileName is the committed release intent file.
const releaseFileName = "release.json"

// MarkerRepo identifies the hosting repository.
type MarkerRepo struct {
	Host  string `json:"host,omitempty"`
	Owner string `json:"owner,omitempty"`
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Valid reports whether the repository metadata is complete.
func (r MarkerRepo) Valid() bool { return r.Owner != "" && r.Name != "" }

// MarkerVerified records the pre-release verification result.
type MarkerVerified struct {
	Commands []string  `json:"commands"`
	Skipped  bool      `json:"skipped"`
	At       time.Time `json:"at"`
}

// MarkerPackage is one planned package release.
type MarkerPackage struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Resolver string `json:"resolver"`
	Current  string `json:"current"`
	Next     string `json:"next"`
	Bump     string `json:"bump"`
	Publish  bool   `json:"publish"`
}

// MarkerChange is one consumed change record.
type MarkerChange struct {
	File       string `json:"file"`
	Type       string `json:"type"`
	Scope      string `json:"scope,omitempty"`
	Bump       string `json:"bump"`
	Body       string `json:"body"`
	Commit     string `json:"commit,omitempty"`
	CommitURL  string `json:"commit_url,omitempty"`
	PullNumber int    `json:"pull_number,omitempty"`
	PullURL    string `json:"pull_url,omitempty"`
	Author     string `json:"author,omitempty"`
}

// Marker is the committed release plan.
type Marker struct {
	SchemaVersion int             `json:"schema_version"`
	Version       string          `json:"version"`
	CreatedAt     time.Time       `json:"created_at"`
	Mode          string          `json:"mode"`
	Fingerprint   string          `json:"fingerprint"`
	Repo          MarkerRepo      `json:"repo"`
	Verified      MarkerVerified  `json:"verified"`
	Packages      []MarkerPackage `json:"packages"`
	Changes       []MarkerChange  `json:"changes"`
}

// MarkerPath returns the marker path for a repository root.
func MarkerPath(root string) string {
	return filepath.Join(root, metadataDirName, releaseFileName)
}

// LoadMarker reads the marker; a missing file returns (nil, nil).
func LoadMarker(root string) (*Marker, error) {
	data, err := fsx.ReadOrEmpty(MarkerPath(root))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, fmt.Errorf("%s: %w", MarkerPath(root), err)
	}
	return &marker, nil
}

// SaveMarker writes the marker atomically.
func SaveMarker(root string, marker *Marker) error {
	if marker.SchemaVersion == 0 {
		marker.SchemaVersion = 1
	}
	if marker.CreatedAt.IsZero() {
		marker.CreatedAt = time.Now()
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsx.WriteFileAtomic(MarkerPath(root), data, 0o644)
}

// RemoveMarker deletes the marker if present.
func RemoveMarker(root string) error {
	err := os.Remove(MarkerPath(root))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
