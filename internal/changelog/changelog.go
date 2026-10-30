// Package changelog renders and updates CHANGELOG.md.
package changelog

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// Entry is one change line in a release section.
type Entry struct {
	Type       string
	Body       string
	CommitSHA  string
	CommitURL  string
	PullNumber int
	PullURL    string
	PullAuthor string
	SortKey    string
}

// Section is one version section. Package is set for package-scoped releases
// in a multi-package repository; the marker key becomes <package>@<version>.
type Section struct {
	Version    string
	Package    string
	Date       time.Time
	Entries    []Entry
	CompareURL string
}

// Key is the marker identity of a section.
func (s Section) Key() string {
	if s.Package == "" {
		return s.Version
	}
	return s.Package + "@" + s.Version
}

var typeOrder = []string{"feat", "fix", "perf", "refactor", "docs", "test", "build", "ci", "chore", "style", "revert"}

// ReleaseEnd closes a version block.
const ReleaseEnd = "<!-- qegg:release:end -->"

var releaseStartRE = regexp.MustCompile(`^<!-- qegg:release v?([^\s]+) -->$`)

// Render returns the full marked block for a section.
func Render(sec Section) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- qegg:release %s -->\n", sec.Key())
	renderBody(&b, sec)
	b.WriteString(ReleaseEnd)
	b.WriteString("\n")
	return b.String()
}

// RenderBody renders a section without the HTML markers qegg uses to locate a
// release block, so pull request bodies carry only the release notes.
func RenderBody(sec Section) string {
	var b strings.Builder
	renderBody(&b, sec)
	return b.String()
}

func renderBody(b *strings.Builder, sec Section) {
	if sec.Package != "" {
		fmt.Fprintf(b, "## %s v%s - %s\n", sec.Package, sec.Version, sec.Date.Format("2006-01-02"))
	} else {
		fmt.Fprintf(b, "## v%s - %s\n", sec.Version, sec.Date.Format("2006-01-02"))
	}
	grouped := groupEntries(sec.Entries)
	for _, typ := range typeOrder {
		entries := grouped[typ]
		if len(entries) == 0 {
			continue
		}
		title := i18n.T("changelog.group." + typ)
		fmt.Fprintf(b, "\n### %s\n\n", title)
		for _, entry := range entries {
			b.WriteString(renderEntry(entry))
			b.WriteString("\n")
		}
	}
	if sec.CompareURL != "" {
		fmt.Fprintf(b, "\n**Full Changelog**: %s\n", sec.CompareURL)
	}
}

func renderEntry(e Entry) string {
	body := strings.TrimSpace(e.Body)
	ref := pullReference(e)
	if e.CommitSHA != "" {
		short := e.CommitSHA
		if len(short) > 7 {
			short = short[:7]
		}
		if e.CommitURL != "" {
			if body == "" {
				return fmt.Sprintf("- [`%s`](%s)%s", short, e.CommitURL, ref)
			}
			return fmt.Sprintf("- [`%s`](%s): %s%s", short, e.CommitURL, body, ref)
		}
		if body == "" {
			return fmt.Sprintf("- `%s`%s", short, ref)
		}
		return fmt.Sprintf("- `%s`: %s%s", short, body, ref)
	}
	return "- " + body + ref
}

// pullReference renders the review anchor of an entry. Many entries can share
// one squash commit, so the pull request is the useful identity.
func pullReference(e Entry) string {
	if e.PullNumber <= 0 || e.PullURL == "" {
		return ""
	}
	if e.PullAuthor == "" {
		return fmt.Sprintf(" ([#%d](%s))", e.PullNumber, e.PullURL)
	}
	return fmt.Sprintf(" ([#%d](%s) by @%s)", e.PullNumber, e.PullURL, e.PullAuthor)
}

func groupEntries(entries []Entry) map[string][]Entry {
	grouped := map[string][]Entry{}
	for _, e := range entries {
		grouped[e.Type] = append(grouped[e.Type], e)
	}
	for typ := range grouped {
		sort.SliceStable(grouped[typ], func(i, j int) bool {
			return grouped[typ][i].SortKey < grouped[typ][j].SortKey
		})
	}
	return grouped
}

// Update inserts or replaces the section whose key matches sec.
func Update(existing []byte, sec Section) ([]byte, error) {
	text := strings.ReplaceAll(string(existing), "\r\n", "\n")
	lines := []string{}
	if strings.TrimSpace(text) != "" {
		lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
	}
	block := strings.Split(strings.TrimRight(Render(sec), "\n"), "\n")

	key := sec.Key()
	start := -1
	for i, line := range lines {
		m := releaseStartRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || m[1] != key {
			continue
		}
		start = i
		break
	}
	if start >= 0 {
		end := -1
		for j := start + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == ReleaseEnd {
				end = j
				break
			}
		}
		if end < 0 {
			return nil, fmt.Errorf("version %s block is missing %s", key, ReleaseEnd)
		}
		out := append([]string{}, lines[:start]...)
		out = append(out, block...)
		out = append(out, lines[end+1:]...)
		return finish(out), nil
	}

	insertAt := -1
	for i, line := range lines {
		if releaseStartRE.MatchString(strings.TrimSpace(line)) {
			insertAt = i
			break
		}
	}
	if insertAt < 0 {
		for i, line := range lines {
			if strings.HasPrefix(line, "## ") {
				insertAt = i
				break
			}
		}
	}

	var out []string
	if insertAt >= 0 {
		out = append(out, lines[:insertAt]...)
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, block...)
		out = append(out, "")
		out = append(out, lines[insertAt:]...)
		return finish(out), nil
	}

	if !hasTitle(lines) {
		out = append(out, "# Changelog", "")
	}
	out = append(out, lines...)
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	out = append(out, block...)
	return finish(out), nil
}

// UpdateAll inserts or replaces every section. The sections keep their order:
// because every insertion lands above the previous block, they are written
// back to front.
func UpdateAll(existing []byte, sections []Section) ([]byte, error) {
	out := existing
	for i := len(sections) - 1; i >= 0; i-- {
		updated, err := Update(out, sections[i])
		if err != nil {
			return nil, err
		}
		out = updated
	}
	return out, nil
}

func finish(lines []string) []byte {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func hasTitle(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			return true
		}
	}
	return false
}

// Extract returns the rendered body of a section without markers. The key is
// the section key: a version, or <package>@<version>.
func Extract(existing []byte, key string) (string, bool) {
	text := strings.ReplaceAll(string(existing), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	start := -1
	for i, line := range lines {
		m := releaseStartRE.FindStringSubmatch(strings.TrimSpace(line))
		if m != nil && m[1] == key {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := -1
	for j := start + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == ReleaseEnd {
			end = j
			break
		}
	}
	if end < 0 {
		return "", false
	}
	body := strings.Trim(strings.Join(lines[start+1:end], "\n"), "\n")
	return body, true
}

// SectionTitle returns the rendered heading of a version.
func SectionTitle(sec Section) string {
	if sec.Package != "" {
		return fmt.Sprintf("%s v%s - %s", sec.Package, sec.Version, sec.Date.Format("2006-01-02"))
	}
	return fmt.Sprintf("v%s - %s", sec.Version, sec.Date.Format("2006-01-02"))
}
