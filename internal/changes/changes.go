// Package changes parses and writes .changes records.
package changes

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// ValidTypes lists supported change types in changelog order.
var ValidTypes = []string{"feat", "fix", "perf", "refactor", "docs", "test", "build", "ci", "chore", "style", "revert"}

var validType = func() map[string]bool {
	m := make(map[string]bool, len(ValidTypes))
	for _, t := range ValidTypes {
		m[t] = true
	}
	return m
}()

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.md$`)

// maxSlugWords is the number of dash separated words a record file name may carry.
const maxSlugWords = 3

// Record is one parsed change record.
type Record struct {
	Path     string
	Type     string
	Scope    string
	Packages map[string]semver.Bump
	Release  bool
	Body     string
}

type frontMatter struct {
	Type     string            `yaml:"type"`
	Scope    string            `yaml:"scope"`
	Packages map[string]string `yaml:"packages"`
	Release  *bool             `yaml:"release"`
}

// Parse reads a record from r.
func Parse(filename string, r io.Reader) (*Record, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	rec, err := parseBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	rec.Path = filename
	return rec, nil
}

func parseBytes(raw []byte) (*Record, error) {
	raw = bytes.TrimPrefix(raw, []byte("ï»¿"))
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	var separators []int
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			separators = append(separators, i)
		}
	}
	if len(separators) == 0 {
		return nil, errors.New("missing front matter separator (---)")
	}

	var fmText, bodyText string
	if separators[0] == 0 {
		if len(separators) < 2 {
			return nil, errors.New("front matter is not terminated by ---")
		}
		fmText = strings.Join(lines[1:separators[1]], "\n")
		bodyText = strings.Join(lines[separators[1]+1:], "\n")
	} else {
		fmText = strings.Join(lines[:separators[0]], "\n")
		bodyText = strings.Join(lines[separators[0]+1:], "\n")
	}

	var fm frontMatter
	dec := yaml.NewDecoder(strings.NewReader(fmText))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("front matter: %w", err)
	}

	rec := &Record{
		Type:     strings.TrimSpace(fm.Type),
		Scope:    strings.TrimSpace(fm.Scope),
		Packages: map[string]semver.Bump{},
		Release:  true,
		Body:     strings.TrimSpace(bodyText),
	}
	if fm.Release != nil {
		rec.Release = *fm.Release
	}
	for name, level := range fm.Packages {
		bump, err := semver.ParseBump(level)
		if err != nil {
			return nil, fmt.Errorf("package %q: %w", name, err)
		}
		rec.Packages[strings.TrimSpace(name)] = bump
	}
	return rec, nil
}

// Validate checks required fields and that referenced packages are registered.
func (r *Record) Validate(known []string) error {
	if r.Type == "" {
		return errors.New("type is required")
	}
	if !validType[r.Type] {
		return fmt.Errorf("unknown type %q (valid: %s)", r.Type, strings.Join(ValidTypes, ", "))
	}
	if len(r.Packages) == 0 {
		return errors.New("packages must not be empty")
	}
	if r.Body == "" {
		return errors.New("body must not be empty")
	}
	registered := make(map[string]bool, len(known))
	for _, name := range known {
		registered[name] = true
	}
	for name := range r.Packages {
		if !registered[name] {
			return fmt.Errorf("package %q is not registered (available: %s)", name, strings.Join(known, ", "))
		}
	}
	if r.Path != "" {
		base := filepath.Base(r.Path)
		if !slugPattern.MatchString(base) {
			return fmt.Errorf("file name %q must match slug.md (lowercase letters, digits, dashes)", base)
		}
		parts := strings.Split(strings.TrimSuffix(base, ".md"), "-")
		if len(parts) > maxSlugWords {
			return fmt.Errorf("file name %q should use at most three words", base)
		}
	}
	return nil
}

// LoadDir parses every markdown record in dir.
func LoadDir(dir string) ([]*Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if strings.EqualFold(entry.Name(), "README.md") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	records := make([]*Record, 0, len(names))
	for _, name := range names {
		full := filepath.Join(dir, name)
		file, err := os.Open(full)
		if err != nil {
			return nil, err
		}
		rec, err := Parse(full, file)
		file.Close()
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

// Consumed returns records that take part in the next release.
func Consumed(records []*Record) []*Record {
	out := make([]*Record, 0, len(records))
	for _, rec := range records {
		if rec.Release {
			out = append(out, rec)
		}
	}
	return out
}

// NextBump aggregates all releasing records into one bump level.
func NextBump(records []*Record) semver.Bump {
	var result semver.Bump
	for _, rec := range records {
		if !rec.Release {
			continue
		}
		for _, bump := range rec.Packages {
			result = semver.Max(result, bump)
		}
	}
	return result
}

// Render serialises a record for writing to disk.
func Render(rec *Record) ([]byte, error) {
	packages := make(map[string]string, len(rec.Packages))
	names := make([]string, 0, len(rec.Packages))
	for name, bump := range rec.Packages {
		packages[name] = string(bump)
		names = append(names, name)
	}
	sort.Strings(names)
	fm := struct {
		Type     string            `yaml:"type"`
		Scope    string            `yaml:"scope,omitempty"`
		Packages map[string]string `yaml:"packages"`
		Release  *bool             `yaml:"release,omitempty"`
	}{Type: rec.Type, Scope: rec.Scope, Packages: packages}
	if !rec.Release {
		release := false
		fm.Release = &release
	}
	var yamlBuf bytes.Buffer
	encoder := yaml.NewEncoder(&yamlBuf)
	encoder.SetIndent(2)
	if err := encoder.Encode(fm); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	body := yamlBuf.Bytes()
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(body)
	buf.WriteString("---\n\n")
	buf.WriteString(strings.TrimSpace(rec.Body))
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// Slugify turns a title into a valid record file name.
func Slugify(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "change"
	}
	// The validator rejects longer names, so a long title keeps its first words.
	if parts := strings.Split(slug, "-"); len(parts) > maxSlugWords {
		slug = strings.Join(parts[:maxSlugWords], "-")
	}
	return slug + ".md"
}

// FileIn returns the absolute path of a record inside dir.
func FileIn(dir, slug string) string {
	return filepath.Join(dir, path.Base(slug))
}
