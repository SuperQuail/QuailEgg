package assets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// checksumsFile is the digest list qegg writes for the collected assets.
const checksumsFile = "checksums.txt"

// ProvenanceFile is the manifest a prebuilt assets directory carries. It names
// the commit the files were built from and, optionally, their digests.
const ProvenanceFile = "qegg-assets.json"

// Provenance describes where a prebuilt assets directory came from. The managed
// workflow builds on other runners and hands the files over as a directory, so
// the manifest is the only evidence tying an attachment to a commit.
type Provenance struct {
	SchemaVersion int               `json:"schema_version"`
	SourceCommit  string            `json:"source_commit"`
	SHA256        map[string]string `json:"sha256,omitempty"`
}

// LoadProvenance reads the manifest of an assets directory; a directory without
// one returns (nil, nil), which lets the caller decide whether it is required.
func LoadProvenance(dir string) (*Provenance, error) {
	data, err := fsx.ReadOrEmpty(filepath.Join(dir, ProvenanceFile))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var provenance Provenance
	if err := json.Unmarshal(data, &provenance); err != nil {
		return nil, errors.New(i18n.Tf("release.err.assets_provenance_form", ProvenanceFile, err))
	}
	return &provenance, nil
}

// Verify checks the claimed source commit and the digest of every file the
// directory contributes. A digest is not optional: a manifest that names the
// right commit but covers the files with nothing would let content from another
// tree through under a correct commit name.
func (p *Provenance) Verify(dir, sourceCommit string) error {
	if !sameHash(p.SourceCommit, sourceCommit) {
		return errors.New(i18n.Tf("release.err.assets_provenance_source", shortHash(p.SourceCommit), shortHash(sourceCommit)))
	}
	names, err := payloadNames(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New(i18n.Tf("release.err.assets_provenance_empty", ProvenanceFile))
	}
	if len(p.SHA256) == 0 {
		return errors.New(i18n.Tf("release.err.assets_provenance_digest_missing", ProvenanceFile, names[0]))
	}
	for _, name := range names {
		want := strings.TrimSpace(p.SHA256[name])
		if want == "" {
			return errors.New(i18n.Tf("release.err.assets_provenance_digest_missing", ProvenanceFile, name))
		}
		digest, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if !strings.EqualFold(digest, want) {
			return errors.New(i18n.Tf("release.err.assets_provenance_digest", name))
		}
	}
	return nil
}

// payloadNames lists the files Verify has to cover: everything that will be
// uploaded. The manifest describes the directory and checksums.txt is rewritten
// from the collected files, so neither carries a recorded digest.
func payloadNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == ProvenanceFile || name == checksumsFile {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// sameHash reports whether two hashes name the same object, tolerating an
// abbreviated one.
func sameHash(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	if len(a) < 7 || len(b) < 7 {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// shortHash trims a hash for a message.
func shortHash(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return "?"
	}
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
