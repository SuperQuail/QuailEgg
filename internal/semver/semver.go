// Package semver implements SemVer bumps used by release planning.
package semver

import (
	"fmt"
	"strings"

	mastersemver "github.com/Masterminds/semver/v3"
)

// Bump is a release level.
type Bump string

// Supported release levels.
const (
	Patch Bump = "patch"
	Minor Bump = "minor"
	Major Bump = "major"
)

// Rank orders bumps so that the highest level wins.
func (b Bump) Rank() int {
	switch b {
	case Major:
		return 3
	case Minor:
		return 2
	case Patch:
		return 1
	default:
		return 0
	}
}

// Valid reports whether b is a known release level.
func (b Bump) Valid() bool { return b.Rank() > 0 }

// ParseBump converts a text level into a Bump.
func ParseBump(s string) (Bump, error) {
	b := Bump(strings.TrimSpace(s))
	if !b.Valid() {
		return "", fmt.Errorf("invalid bump level %q (want patch, minor or major)", s)
	}
	return b, nil
}

// Max returns the higher of two bumps.
func Max(a, b Bump) Bump {
	if a.Rank() >= b.Rank() {
		return a
	}
	return b
}

// Parse parses a SemVer version.
func Parse(s string) (mastersemver.Version, error) {
	v, err := mastersemver.NewVersion(strings.TrimSpace(s))
	if err != nil {
		return mastersemver.Version{}, fmt.Errorf("invalid version %q: %w", s, err)
	}
	return *v, nil
}

// Next applies bump to current and returns the next version.
func Next(current string, bump Bump) (mastersemver.Version, error) {
	v, err := Parse(current)
	if err != nil {
		return mastersemver.Version{}, err
	}
	switch bump {
	case Major:
		return v.IncMajor(), nil
	case Minor:
		return v.IncMinor(), nil
	case Patch:
		return v.IncPatch(), nil
	default:
		return mastersemver.Version{}, fmt.Errorf("invalid bump level %q", bump)
	}
}

// Tag joins a tag prefix with a version.
func Tag(prefix, v string) string {
	return prefix + strings.TrimPrefix(v, "v")
}

// Compare returns -1, 0 or 1 comparing a with b.
func Compare(a, b string) (int, error) {
	va, err := Parse(a)
	if err != nil {
		return 0, err
	}
	vb, err := Parse(b)
	if err != nil {
		return 0, err
	}
	return va.Compare(&vb), nil
}
