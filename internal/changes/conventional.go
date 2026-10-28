package changes

import (
	"regexp"
	"strings"
)

// conventionalSubject matches a conventional commit subject such as
// "feat(cache): support configurable ttl".
var conventionalSubject = regexp.MustCompile("^(feat|fix|perf|docs|refactor|test|build|ci|chore|style|revert)(\\(([^)]+)\\))?!?:\\s*(.*)$")

// SplitConventional splits a conventional commit message into its type, scope
// and description. ok is false when the message does not follow the
// conventional commit format.
func SplitConventional(message string) (typ, scope, description string, ok bool) {
	m := conventionalSubject.FindStringSubmatch(message)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[3], strings.TrimSpace(m[4]), true
}
