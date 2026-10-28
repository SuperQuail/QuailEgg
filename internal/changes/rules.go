package changes

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// IsWithin reports whether the slash-separated file lives inside dir.
func IsWithin(file, dir string) bool {
	if dir == "." || dir == "" {
		return true
	}
	return file == dir || strings.HasPrefix(file, dir+"/")
}

// MatchesScope reports whether file is covered by the configured scope
// directories. An empty scope means the whole repository.
func MatchesScope(file string, scope []string) bool {
	if len(scope) == 0 {
		return true
	}
	clean := path.Clean(filepath.ToSlash(file))
	for _, dir := range scope {
		dir = path.Clean(filepath.ToSlash(dir))
		if dir == "." || IsWithin(clean, dir) {
			return true
		}
	}
	return false
}

// Staged is the input of the pre-commit rule.
type Staged struct {
	Root   string   // repository root used to read the staged files
	Dir    string   // change record directory, relative to Root
	Scope  []string // directories watched by the rule
	Ignore []string // files qegg writes itself; never require a record
	Known  []string // package names a record may reference
	Files  []string // staged paths, relative to Root and slash-separated
}

// Enforce validates that source changes come with a valid change record.
func Enforce(in Staged) error {
	if len(in.Files) == 0 {
		return nil
	}
	recordsDir := path.Clean(filepath.ToSlash(in.Dir))
	sourceTouched := false
	var records []string
	for _, file := range in.Files {
		clean := path.Clean(filepath.ToSlash(file))
		if isIgnored(clean, in.Ignore) {
			continue
		}
		if IsWithin(clean, recordsDir) {
			if isRecord(clean) {
				records = append(records, clean)
			}
			continue
		}
		if MatchesScope(clean, in.Scope) {
			sourceTouched = true
		}
	}
	if !sourceTouched {
		return nil
	}
	if len(records) == 0 {
		return fmt.Errorf(i18n.T("hook.missing_record"), in.Dir)
	}
	for _, record := range records {
		parsed, err := parseFile(filepath.Join(in.Root, filepath.FromSlash(record)))
		if err != nil {
			return err
		}
		if err := parsed.Validate(in.Known); err != nil {
			return fmt.Errorf("%s: %w", record, err)
		}
	}
	return nil
}

// isIgnored reports whether file is one qegg writes itself.
func isIgnored(file string, ignore []string) bool {
	for _, pattern := range ignore {
		clean := path.Clean(filepath.ToSlash(pattern))
		if clean == "." || clean == "" {
			continue
		}
		if file == clean || strings.HasPrefix(file, clean+"/") {
			return true
		}
	}
	return false
}

func isRecord(file string) bool {
	return strings.HasSuffix(file, ".md") && !strings.EqualFold(path.Base(file), "README.md")
}

func parseFile(abs string) (*Record, error) {
	file, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(abs, file)
}
