package python

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// pythonVersionRE captures X.Y.Z plus an optional PEP 440 prerelease part.
// Accepted spellings include 2.0.2, 2.0.2-a14, 2.0.2-alpha.14, 2.0.2a14 and
// 2.0.2rc1.
var pythonVersionRE = regexp.MustCompile("^([0-9]+)\\.([0-9]+)\\.([0-9]+)(?:[-.]?([A-Za-z]+)[-.]?([0-9]+))?$")

// versionLabels maps accepted PEP 440 pre-release spellings to qegg channels and
// the canonical suffix used in tags and registry URLs.
var versionLabels = map[string]struct {
	channel string
	suffix  string
}{
	"a":       {"alpha", "a"},
	"alpha":   {"alpha", "a"},
	"b":       {"beta", "b"},
	"beta":    {"beta", "b"},
	"c":       {"rc", "rc"},
	"rc":      {"rc", "rc"},
	"pre":     {"rc", "rc"},
	"preview": {"rc", "rc"},
}

// parsePythonVersion splits a version into its triple and optional prerelease.
func parsePythonVersion(version string) (base, channel string, counter int, prerelease, ok bool) {
	m := pythonVersionRE.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return "", "", 0, false, false
	}
	base = m[1] + "." + m[2] + "." + m[3]
	if m[4] == "" {
		return base, "", 0, false, true
	}
	label, found := versionLabels[strings.ToLower(m[4])]
	if !found {
		return "", "", 0, false, false
	}
	n, err := strconv.Atoi(m[5])
	if err != nil || n < 1 {
		return "", "", 0, false, false
	}
	return base, label.channel, n, true, true
}

// canonicalVersion renders a version the way PyPI normalises it.
func canonicalVersion(version string) string {
	base, channel, counter, prerelease, ok := parsePythonVersion(version)
	if !ok || !prerelease {
		return version
	}
	suffix, _ := channelSuffix(channel)
	return base + suffix + strconv.Itoa(counter)
}

func channelSuffix(channel string) (string, bool) {
	for _, label := range versionLabels {
		if label.channel == channel {
			return label.suffix, true
		}
	}
	return "", false
}

// Prerelease renders a prerelease version in the pyproject spelling.
func (driver) Prerelease(base, channel string, counter int) (string, error) {
	if counter < 1 {
		return "", fmt.Errorf("prerelease counter %d must be positive", counter)
	}
	if _, _, _, prerelease, ok := parsePythonVersion(base); !ok || prerelease {
		return "", fmt.Errorf("base version %q is not a stable X.Y.Z version", base)
	}
	suffix, ok := channelSuffix(channel)
	if !ok {
		return "", fmt.Errorf("unknown prerelease channel %q", channel)
	}
	return base + "-" + suffix + strconv.Itoa(counter), nil
}

// SplitPrerelease parses either the manifest or the tag spelling.
func (driver) SplitPrerelease(version string) (base, channel string, counter int, ok bool) {
	b, c, n, prerelease, valid := parsePythonVersion(version)
	if !valid || !prerelease {
		return "", "", 0, false
	}
	return b, c, n, true
}

// TagVersion renders the version embedded in a tag (PEP 440 spelling).
func (driver) TagVersion(version string) string {
	return canonicalVersion(version)
}
