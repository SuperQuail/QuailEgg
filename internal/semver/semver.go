// Package semver implements SemVer bumps used by release planning.
package semver

import (
	"fmt"
	"strconv"
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

// Channels lists the prerelease channels qegg supports. The order is the
// order a project is expected to walk them in.
var Channels = []string{"alpha", "beta", "rc"}

// ValidChannel reports whether channel is a supported prerelease channel.
func ValidChannel(channel string) bool {
	for _, candidate := range Channels {
		if candidate == channel {
			return true
		}
	}
	return false
}

// IsPrerelease reports whether v carries a prerelease part.
func IsPrerelease(v string) bool {
	parsed, err := Parse(v)
	return err == nil && parsed.Prerelease() != ""
}

// Stable strips the prerelease part of v and reports whether it had one.
func Stable(v string) (string, bool) {
	parsed, err := Parse(v)
	if err != nil {
		return v, false
	}
	if parsed.Prerelease() == "" {
		return v, false
	}
	return plain(&parsed), true
}

// SplitPrerelease splits a version such as 0.5.0-alpha.2 into its base,
// channel and counter. It reports false for a stable version, an unknown
// channel or a counter that is not a positive integer.
func SplitPrerelease(v string) (base, channel string, counter int, ok bool) {
	parsed, err := Parse(v)
	if err != nil {
		return "", "", 0, false
	}
	pre := parsed.Prerelease()
	if pre == "" {
		return "", "", 0, false
	}
	channel, rest, found := strings.Cut(pre, ".")
	if !found || !ValidChannel(channel) {
		return "", "", 0, false
	}
	counter, err = strconv.Atoi(rest)
	if err != nil || counter < 1 {
		return "", "", 0, false
	}
	return plain(&parsed), channel, counter, true
}

// Prerelease builds the version base-channel.counter.
func Prerelease(base, channel string, counter int) (string, error) {
	if !ValidChannel(channel) {
		return "", fmt.Errorf("unknown prerelease channel %q (want %s)", channel, strings.Join(Channels, ", "))
	}
	if counter < 1 {
		return "", fmt.Errorf("prerelease counter %d must be positive", counter)
	}
	parsed, err := Parse(base)
	if err != nil {
		return "", err
	}
	if parsed.Prerelease() != "" {
		return "", fmt.Errorf("base version %q already has a prerelease part", base)
	}
	return fmt.Sprintf("%s-%s.%d", plain(&parsed), channel, counter), nil
}

// plain renders the major.minor.patch triple of v without its prerelease part.
func plain(v *mastersemver.Version) string {
	return fmt.Sprintf("%d.%d.%d", v.Major(), v.Minor(), v.Patch())
}
