package changes

import (
	"bytes"
	"fmt"
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

// TouchesScope reports whether any of files is covered by the record rule.
// Managed files and the change record directory itself never count.
func TouchesScope(files []string, dir string, scope []string, ignore []string) bool {
	recordsDir := path.Clean(filepath.ToSlash(dir))
	for _, file := range files {
		clean := path.Clean(filepath.ToSlash(file))
		if isIgnored(clean, ignore) || IsWithin(clean, recordsDir) {
			continue
		}
		if MatchesScope(clean, scope) {
			return true
		}
	}
	return false
}

// Status is the status a snapshot diff reports for one path. The rule only has
// to know whether the path still exists in the snapshot under review, so every
// status but StatusDeleted keeps it.
type Status string

// Statuses the rule distinguishes. The letters are the ones git reports.
const (
	// StatusAdded marks a path the snapshot adds.
	StatusAdded Status = "A"
	// StatusModified marks a path the snapshot changes.
	StatusModified Status = "M"
	// StatusDeleted marks a path the snapshot removes. It is the only status
	// that takes the path out of the snapshot.
	StatusDeleted Status = "D"
)

// Change is one path that differs between the snapshot under review and the
// snapshot it is compared against.
type Change struct {
	Path   string
	Status Status
}

// Snapshot is the input of the change record rule: the changed paths of the
// snapshot being checked and everything the rule needs to judge them. The
// pre-commit hook passes the index, the pull request check passes the tree of
// the commit the event names, so neither judges a working tree an author may
// have edited after staging.
type Snapshot struct {
	Dir    string   // change record directory, slash separated
	Scope  []string // directories watched by the rule
	Ignore []string // files qegg writes itself; never require a record
	Known  []string // package names a record may reference
	// Changes lists the changed paths of the snapshot, slash separated.
	Changes []Change
	// Read returns the body of a record as the snapshot stores it. It is
	// required: the rule never falls back to the working tree, because a caller
	// that forgot to say which snapshot it judges would then read a tree the
	// commit or the pull request will not contain.
	Read func(rel string) ([]byte, error)
}

// MissingRecordError reports that the snapshot changes release-scoped files
// without carrying a record the rule accepts. Dir is the change record
// directory. The wording is the commit-time one because the hook prints the
// error as it is; the pull request check reports the same failure with its own
// message and code.
type MissingRecordError struct {
	Dir string
}

// Error implements error.
func (e *MissingRecordError) Error() string {
	return i18n.Tf("hook.missing_record", e.Dir)
}

// InvalidRecordError reports a record the snapshot carries that does not
// validate. Path is relative to the snapshot root.
type InvalidRecordError struct {
	Path string
	Err  error
}

// Error implements error. Parse already names the record it could not read, so
// the path is added only when the wrapped error does not carry it yet.
func (e *InvalidRecordError) Error() string {
	message := e.Err.Error()
	if strings.HasPrefix(message, e.Path+": ") {
		return message
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

// Unwrap exposes the validation failure.
func (e *InvalidRecordError) Unwrap() error { return e.Err }

// Enforce validates that the snapshot's release-scoped changes come with a
// record the same snapshot still carries. The pre-commit hook and the pull
// request check both call it, so scope matching, the ignore list, record
// recognition and content validation cannot drift apart.
//
// A record counts when the snapshot adds or changes it. A deletion never counts,
// so removing an old record is not a way to satisfy the rule. A rename arrives
// as a deletion of the old path plus an addition of the new one, because the
// diffs feeding this rule disable rename detection: the record at its new path
// counts and the old path does not, which is what the author intended.
//
// The returned paths are the records that satisfied the rule, which the pull
// request check reports so a reader can see what it accepted.
func Enforce(in Snapshot) ([]string, error) {
	if len(in.Changes) == 0 {
		return nil, nil
	}
	recordsDir := path.Clean(filepath.ToSlash(in.Dir))
	sourceTouched := false
	var records []string
	for _, change := range in.Changes {
		clean := path.Clean(filepath.ToSlash(change.Path))
		if isIgnored(clean, in.Ignore) {
			continue
		}
		if IsWithin(clean, recordsDir) {
			// A record only counts when the snapshot still carries it: removing
			// an old record must never stand in for writing a new one.
			if IsRecord(clean, recordsDir) && change.Status != StatusDeleted {
				records = append(records, clean)
			}
			continue
		}
		if MatchesScope(clean, in.Scope) {
			sourceTouched = true
		}
	}
	if !sourceTouched {
		return nil, nil
	}
	if len(records) == 0 {
		return nil, &MissingRecordError{Dir: in.Dir}
	}
	for _, record := range records {
		parsed, err := in.parse(record)
		if err != nil {
			return nil, &InvalidRecordError{Path: record, Err: err}
		}
		if err := parsed.Validate(in.Known); err != nil {
			return nil, &InvalidRecordError{Path: record, Err: err}
		}
	}
	return records, nil
}

// IsRecord reports whether the slash-separated path is a change record the
// release pipeline reads: a markdown file directly inside dir, except
// README.md. LoadDir reads that one level, so a record in a subdirectory never
// reaches the changelog and must not satisfy the rule either.
func IsRecord(file, dir string) bool {
	return path.Dir(file) == dir &&
		strings.HasSuffix(file, ".md") &&
		!strings.EqualFold(path.Base(file), "README.md")
}

// parse reads one record of the snapshot. The path inside the snapshot is the
// record name, so parse errors name the file the reader asked for.
func (in Snapshot) parse(rel string) (*Record, error) {
	body, err := in.read(rel)
	if err != nil {
		return nil, err
	}
	return Parse(rel, bytes.NewReader(body))
}

// read returns the body of rel as the snapshot stores it. A snapshot without a
// reader is a programming error the rule reports instead of reading somewhere
// else.
func (in Snapshot) read(rel string) ([]byte, error) {
	if in.Read == nil {
		return nil, fmt.Errorf("changes: the snapshot has no reader for %s", rel)
	}
	return in.Read(rel)
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
