package state

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// ReleaseIdentity names one release across runs, runners and machines: the
// repository, the tags it publishes, its version, channel, mode and the marker
// fingerprint that changes whenever the release intent changes.
//
// SourceCommit pins the commit the release is built from. It is deliberately not
// part of Key: the identity has to keep naming the same release before and after
// the snapshot is known.
type ReleaseIdentity struct {
	SchemaVersion int      `json:"schema_version"`
	Repo          string   `json:"repo,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Version       string   `json:"version,omitempty"`
	Channel       string   `json:"channel,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	Fingerprint   string   `json:"fingerprint"`
	SourceCommit  string   `json:"source_commit,omitempty"`
}

// Key is the stable storage key of the identity.
func (id ReleaseIdentity) Key() string {
	parts := []string{id.Repo, strings.Join(sortedTags(id.Tags), ","), id.Version, id.Mode, id.Fingerprint}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])[:32]
}

// WithSource returns the identity pinned to a commit.
func (id ReleaseIdentity) WithSource(commit string) ReleaseIdentity {
	id.SourceCommit = strings.TrimSpace(commit)
	return id
}

// SameRelease reports whether two identities describe the same release. An
// empty field means "unknown" and matches anything, so a record written by an
// older run still belongs to this release. SourceCommit is not compared: the
// snapshot is what the identity is used to find.
func (id ReleaseIdentity) SameRelease(other ReleaseIdentity) bool {
	fields := [][2]string{
		{id.Repo, other.Repo},
		{id.Version, other.Version},
		{id.Channel, other.Channel},
		{id.Mode, other.Mode},
		{id.Fingerprint, other.Fingerprint},
	}
	for _, field := range fields {
		if field[0] != "" && field[1] != "" && field[0] != field[1] {
			return false
		}
	}
	return sameTags(id.Tags, other.Tags)
}

// sortedTags returns a sorted copy of tags.
func sortedTags(tags []string) []string {
	out := append([]string(nil), tags...)
	sort.Strings(out)
	// A marker that lists the same tag twice still names one release, so the key
	// must not depend on how often a tag is repeated.
	compact := out[:0]
	for i, tag := range out {
		if i > 0 && tag == out[i-1] {
			continue
		}
		compact = append(compact, tag)
	}
	return compact
}

// sameTags compares two tag sets, where an empty side is unknown.
func sameTags(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	left, right := sortedTags(a), sortedTags(b)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
