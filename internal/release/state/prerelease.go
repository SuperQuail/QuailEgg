package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// PreReleaseFileName is the committed lock that puts the repository into a
// prerelease channel. Its presence alone enables prerelease releases.
const PreReleaseFileName = "pre-release.lock"

// PreRelease is the committed prerelease channel state. Channel is the only
// authoritative field; Version mirrors the last planned prerelease version and
// is corrected from the git tags on every run.
type PreRelease struct {
	SchemaVersion int    `json:"schema_version"`
	Channel       string `json:"channel"`
	Version       string `json:"version,omitempty"`
}

// PreReleasePath returns the lock path for a repository root.
func PreReleasePath(root string) string {
	return filepath.Join(root, metadataDirName, PreReleaseFileName)
}

// LoadPreRelease reads the lock; a missing file returns (nil, nil).
func LoadPreRelease(root string) (*PreRelease, error) {
	data, err := fsx.ReadOrEmpty(PreReleasePath(root))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return ParsePreRelease(data, PreReleasePath(root))
}

// ParsePreRelease decodes and validates a lock file.
func ParsePreRelease(data []byte, path string) (*PreRelease, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var pre PreRelease
	if err := dec.Decode(&pre); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if pre.SchemaVersion == 0 {
		pre.SchemaVersion = 1
	}
	if err := pre.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &pre, nil
}

// Validate checks the schema version, the channel and the optional version.
func (p *PreRelease) Validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d (this qegg supports 1)", p.SchemaVersion)
	}
	if !semver.ValidChannel(p.Channel) {
		return fmt.Errorf("channel %q is not one of %s", p.Channel, strings.Join(semver.Channels, ", "))
	}
	if p.Version == "" {
		return nil
	}
	if _, channel, _, ok := semver.SplitPrerelease(p.Version); !ok || channel != p.Channel {
		return fmt.Errorf("version %q is not a %s prerelease", p.Version, p.Channel)
	}
	return nil
}

// SavePreRelease writes the lock atomically.
func SavePreRelease(root string, pre *PreRelease) error {
	if pre.SchemaVersion == 0 {
		pre.SchemaVersion = 1
	}
	if err := pre.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pre, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsx.WriteFileAtomic(PreReleasePath(root), data, 0o644)
}

// RemovePreRelease deletes the lock if present.
func RemovePreRelease(root string) error {
	err := os.Remove(PreReleasePath(root))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
