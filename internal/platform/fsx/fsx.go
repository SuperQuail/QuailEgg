// Package fsx centralises filesystem writes so they stay atomic and verifiable.
package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RepoRoot walks up from start looking for a directory containing .git.
func RepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a git repository")
		}
		dir = parent
	}
}

// FindUp walks up from start looking for name and returns its path.
func FindUp(start, name string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, name)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fs.ErrNotExist
		}
		dir = parent
	}
}

// WriteFileAtomic writes data to path via a temporary file and rename.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Edit describes a file write guarded by the expected current content.
type Edit struct {
	Path string
	// ExpectHash is the hex sha256 of the current content. Empty means the
	// file must not exist yet.
	ExpectHash string
	Content    []byte
	Perm       fs.FileMode
}

// Hash returns the hex sha256 of b.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Apply validates every edit before writing any of them.
func Apply(edits []Edit) error {
	type staged struct {
		edit Edit
	}
	checked := make([]staged, 0, len(edits))
	for _, e := range edits {
		current, err := os.ReadFile(e.Path)
		if e.ExpectHash == "" {
			if err == nil {
				return fmt.Errorf("%s already exists", e.Path)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		} else {
			if err != nil {
				return fmt.Errorf("%s: %w", e.Path, err)
			}
			if Hash(current) != e.ExpectHash {
				return fmt.Errorf("%s changed since it was read", e.Path)
			}
		}
		checked = append(checked, staged{edit: e})
	}
	for _, s := range checked {
		perm := s.edit.Perm
		if perm == 0 {
			perm = 0o644
		}
		if err := WriteFileAtomic(s.edit.Path, s.edit.Content, perm); err != nil {
			return err
		}
	}
	return nil
}

// AppendLine appends line to path unless it is already present.
func AppendLine(path, line string) (bool, error) {
	current, err := os.ReadFile(path)
	changed := false
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	body := string(current)
	for _, existing := range strings.Split(body, "\n") {
		if strings.TrimSpace(existing) == strings.TrimSpace(line) {
			return false, nil
		}
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += line + "\n"
	if err := WriteFileAtomic(path, []byte(body), 0o644); err != nil {
		return false, err
	}
	changed = true
	return changed, nil
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Rel returns slash-separated base-relative path.
func Rel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

// Join resolves a slash-separated relative path against root.
func Join(root, rel string) string {
	if rel == "." || rel == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// ReadOrEmpty returns the file content, or nil when it does not exist.
func ReadOrEmpty(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}
