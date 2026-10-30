// Package versionfile reads and rewrites the version stored in files that are
// not package manifests, such as Tauri's tauri.conf.json. A write replaces only
// the version literal, so the release commit keeps the surrounding formatting.
package versionfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Formats.
const (
	// FormatJSON stores the version in a JSON string member.
	FormatJSON = "json"
	// DefaultKey is the member a JSON version file keeps the version in.
	DefaultKey = "version"
)

// Spec describes one file outside the package manifests that carries the
// release version. The configuration spells it as a [[version.file]] table.
type Spec struct {
	// Path is repository relative and uses forward slashes.
	Path   string `toml:"path"`
	Format string `toml:"format"`
	Key    string `toml:"key"`
}

// Validate checks the spec without reading the file.
func (s Spec) Validate() error {
	if _, err := s.RepoPath(); err != nil {
		return err
	}
	switch s.format() {
	case FormatJSON:
	default:
		return fmt.Errorf("version file %s: unsupported format %q (use %q)", s.Path, s.Format, FormatJSON)
	}
	return nil
}

// RepoPath returns the repository relative path in slash form.
func (s Spec) RepoPath() (string, error) {
	rel := strings.TrimSpace(s.Path)
	if rel == "" {
		return "", errors.New("version file path must not be empty")
	}
	if filepath.IsAbs(rel) || path.IsAbs(filepath.ToSlash(rel)) {
		return "", fmt.Errorf("version file path %q must stay inside the repository", s.Path)
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("version file path %q must stay inside the repository", s.Path)
	}
	return clean, nil
}

// Read returns the version currently stored in the file.
func (s Spec) Read(root string) (string, error) {
	rel, data, err := s.read(root)
	if err != nil {
		return "", err
	}
	switch s.format() {
	case FormatJSON:
		value, _, _, err := jsonMember(data, s.key())
		if err != nil {
			return "", s.memberError(rel, err)
		}
		return value, nil
	}
	return "", fmt.Errorf("version file %s: unsupported format %q", rel, s.Format)
}

// SetVersion writes version into the file and leaves every other byte alone.
func (s Spec) SetVersion(root, version string) error {
	rel, data, err := s.read(root)
	if err != nil {
		return err
	}
	switch s.format() {
	case FormatJSON:
		_, start, end, err := jsonMember(data, s.key())
		if err != nil {
			return s.memberError(rel, err)
		}
		encoded, err := json.Marshal(version)
		if err != nil {
			return err
		}
		next := make([]byte, 0, len(data)-(end-start)+len(encoded))
		next = append(next, data[:start]...)
		next = append(next, encoded...)
		next = append(next, data[end:]...)
		return fsx.WriteFileAtomic(filepath.Join(root, filepath.FromSlash(rel)), next, 0o644)
	}
	return fmt.Errorf("version file %s: unsupported format %q", rel, s.Format)
}

// read loads the file bytes and returns the slash separated path it used.
func (s Spec) read(root string) (string, []byte, error) {
	rel, err := s.RepoPath()
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rel, nil, errors.New(i18n.Tf("versionfile.err.missing", rel))
		}
		return rel, nil, err
	}
	return rel, data, nil
}

// memberError turns a JSON member lookup error into a user facing message.
func (s Spec) memberError(rel string, err error) error {
	switch {
	case errors.Is(err, errNotObject):
		return errors.New(i18n.Tf("versionfile.err.object", rel))
	case errors.Is(err, errKeyMissing), errors.Is(err, errNotString):
		return errors.New(i18n.Tf("versionfile.err.key", rel, s.key()))
	default:
		return errors.New(i18n.Tf("versionfile.err.json", rel, err))
	}
}

// format returns the file format, defaulting to JSON.
func (s Spec) format() string {
	if strings.TrimSpace(s.Format) == "" {
		return FormatJSON
	}
	return strings.TrimSpace(s.Format)
}

// key returns the member that holds the version, defaulting to "version".
func (s Spec) key() string {
	if strings.TrimSpace(s.Key) == "" {
		return DefaultKey
	}
	return strings.TrimSpace(s.Key)
}
