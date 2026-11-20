package java

import (
	"errors"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// errMavenRange marks a range-shaped expression this parser cannot interpret.
// The caller reports it as a finding instead of guessing the answer.
var errMavenRange = errors.New("unparsable maven version range")

// mavenRange is one interval of a Maven dependency version range. An empty
// bound leaves that side open, which is how [1.0,) and (,2.0] are written.
type mavenRange struct {
	lower     string
	upper     string
	lowerOpen bool
	upperOpen bool
}

// parseMavenRange parses a Maven dependency version range. isRange reports
// whether expr is a range at all: a plain version literal is not one, and the
// caller rewrites it the way it always did. A non-nil error with isRange set
// means the expression looks like a range but cannot be read with confidence,
// such as [1.0,2.0 or [1.0.2.0).
func parseMavenRange(expr string) (ranges []mavenRange, isRange bool, err error) {
	text := strings.TrimSpace(expr)
	if text == "" {
		return nil, false, nil
	}
	if !strings.HasPrefix(text, "[") && !strings.HasPrefix(text, "(") {
		// A version literal never contains range punctuation. Anything that
		// does is a form this parser does not understand.
		if strings.ContainsAny(text, "[](),") {
			return nil, true, errMavenRange
		}
		return nil, false, nil
	}
	for i := 0; i < len(text); {
		open := text[i]
		if open != '[' && open != '(' {
			return nil, true, errMavenRange
		}
		closeAt := -1
		for j := i + 1; j < len(text); j++ {
			if text[j] == ']' || text[j] == ')' {
				closeAt = j
				break
			}
		}
		if closeAt < 0 {
			return nil, true, errMavenRange
		}
		interval, ok := parseMavenInterval(text[i:closeAt+1], open, text[closeAt])
		if !ok {
			return nil, true, errMavenRange
		}
		ranges = append(ranges, interval)
		i = closeAt + 1
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
		if i == len(text) {
			break
		}
		if text[i] != ',' {
			return nil, true, errMavenRange
		}
		i++
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
	}
	if len(ranges) == 0 {
		return nil, true, errMavenRange
	}
	return ranges, true, nil
}

// parseMavenInterval parses one [lower,upper] group, including the exact form
// [1.0] that Maven uses for a required version.
func parseMavenInterval(group string, open, close byte) (mavenRange, bool) {
	body := group[1 : len(group)-1]
	lower, upper, hasComma := strings.Cut(body, ",")
	if !hasComma {
		exact := strings.TrimSpace(body)
		if exact == "" || open != '[' || close != ']' {
			return mavenRange{}, false
		}
		return mavenRange{lower: exact, upper: exact}, true
	}
	if strings.Contains(upper, ",") {
		return mavenRange{}, false
	}
	lower, upper = strings.TrimSpace(lower), strings.TrimSpace(upper)
	if lower == "" && upper == "" {
		return mavenRange{}, false
	}
	return mavenRange{
		lower:     lower,
		upper:     upper,
		lowerOpen: open == '(',
		upperOpen: close == ')',
	}, true
}

// rangeContains reports whether version falls inside one of the parsed ranges.
// An unreadable bound returns an error so the caller stops instead of guessing:
// the whole point of the check is to never rewrite a constraint it cannot
// evaluate.
func rangeContains(ranges []mavenRange, version string) (bool, error) {
	for _, interval := range ranges {
		inside, err := interval.contains(version)
		if err != nil {
			return false, err
		}
		if inside {
			return true, nil
		}
	}
	return false, nil
}

// contains reports whether version lies inside one interval.
func (r mavenRange) contains(version string) (bool, error) {
	if r.lower != "" {
		cmp, err := semver.Compare(version, r.lower)
		if err != nil {
			return false, err
		}
		if cmp < 0 || (cmp == 0 && r.lowerOpen) {
			return false, nil
		}
	}
	if r.upper != "" {
		cmp, err := semver.Compare(version, r.upper)
		if err != nil {
			return false, err
		}
		if cmp > 0 || (cmp == 0 && r.upperOpen) {
			return false, nil
		}
	}
	return true, nil
}
