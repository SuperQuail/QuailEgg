package changes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// validRecord satisfies the rule for the "demo" package.
const validRecord = `---
type: feat
packages:
  demo: patch
---

Describe the change the commit carries.
`

// brokenRecord has no front matter at all.
const brokenRecord = "not a record at all\n"

// unknownPackageRecord names a package the configuration does not register.
const unknownPackageRecord = `---
type: feat
packages:
  other: patch
---

Reference a package that is not registered.
`

func writeRulesFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// changesOf builds a change set of paths that all share one status; the empty
// status means "unchanged in kind", which the rule treats as present.
func changesOf(paths []string, status Status) []Change {
	list := make([]Change, 0, len(paths))
	for _, path := range paths {
		list = append(list, Change{Path: path, Status: status})
	}
	return list
}

// staticRead serves one record body for any path the rule asks for.
func staticRead(body string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(body), nil }
}

func TestEnforceIgnoresManagedPaths(t *testing.T) {
	ignore := []string{
		".quailegg.toml",
		".changes/README.md",
		".github/workflows/qegg-release.yml",
		".github/workflows/qegg-pr.yml",
		".github/workflows/qegg-status.yml",
		".git/hooks/pre-commit",
	}
	managed := []string{
		".quailegg.toml",
		".changes/README.md",
		".github/workflows/qegg-release.yml",
	}

	if _, err := Enforce(Snapshot{Dir: ".changes", Ignore: ignore, Changes: changesOf(managed, StatusModified)}); err != nil {
		t.Fatalf("qegg generated files required a record: %v", err)
	}

	files := append(append([]string{}, managed...), "src/cache.js")
	_, err := Enforce(Snapshot{Dir: ".changes", Ignore: ignore, Changes: changesOf(files, StatusModified)})
	var missing *MissingRecordError
	if !errors.As(err, &missing) {
		t.Fatalf("a source change without a record should be rejected, got %v", err)
	}
}

// The rule judges the snapshot it is given, not a working tree copy: the caller
// decides what "the change about to land" is.
func TestEnforceReadsRecordsFromTheSnapshot(t *testing.T) {
	root := t.TempDir()
	// The working tree carries a broken copy the rule must never read.
	writeRulesFile(t, filepath.Join(root, ".changes", "index.md"), brokenRecord)
	read := func(rel string) ([]byte, error) {
		if rel != ".changes/index.md" {
			t.Fatalf("unexpected read of %q", rel)
		}
		return []byte(validRecord), nil
	}
	records, err := Enforce(Snapshot{
		Dir:   ".changes",
		Known: []string{"demo"},
		Changes: []Change{
			{Path: ".changes/index.md", Status: StatusModified},
			{Path: "src/app.js", Status: StatusModified},
		},
		Read: read,
	})
	if err != nil {
		t.Fatalf("the snapshot record is valid: %v", err)
	}
	if len(records) != 1 || records[0] != ".changes/index.md" {
		t.Fatalf("records = %v, want the record the rule accepted", records)
	}
}

// A snapshot that names records it cannot read is reported, never silently read
// from the working tree: that is the failure this rule exists to prevent.
func TestEnforceNeedsASnapshotReader(t *testing.T) {
	_, err := Enforce(Snapshot{
		Dir:   ".changes",
		Known: []string{"demo"},
		Changes: []Change{
			{Path: ".changes/index.md", Status: StatusAdded},
			{Path: "src/app.js", Status: StatusModified},
		},
	})
	var invalid *InvalidRecordError
	if !errors.As(err, &invalid) {
		t.Fatalf("a snapshot without a reader must be reported, got %v", err)
	}
	if invalid.Path != ".changes/index.md" {
		t.Fatalf("InvalidRecordError.Path = %q", invalid.Path)
	}
}

// A working tree that still carries the old record must not hide a snapshot that
// removed it: a deletion is not a record.
func TestEnforceRejectsADeletedRecord(t *testing.T) {
	_, err := Enforce(Snapshot{
		Dir:   ".changes",
		Known: []string{"demo"},
		Changes: []Change{
			{Path: ".changes/old.md", Status: StatusDeleted},
			{Path: "src/app.js", Status: StatusModified},
		},
		Read: func(rel string) ([]byte, error) {
			t.Fatalf("a deleted record must not be read, got %q", rel)
			return nil, os.ErrNotExist
		},
	})
	var missing *MissingRecordError
	if !errors.As(err, &missing) {
		t.Fatalf("deleting a record must not satisfy the rule, got %v", err)
	}
	if missing.Dir != ".changes" {
		t.Fatalf("MissingRecordError.Dir = %q", missing.Dir)
	}
}

// A renamed record reaches the rule as a deletion plus an addition, because the
// diffs that feed it disable rename detection. The record at its new path
// counts, which is what an author who renames a record means.
func TestEnforceCountsARenamedRecord(t *testing.T) {
	_, err := Enforce(Snapshot{
		Dir:   ".changes",
		Known: []string{"demo"},
		Changes: []Change{
			{Path: ".changes/old.md", Status: StatusDeleted},
			{Path: ".changes/new.md", Status: StatusAdded},
			{Path: "src/app.js", Status: StatusModified},
		},
		Read: staticRead(validRecord),
	})
	if err != nil {
		t.Fatalf("a renamed record must satisfy the rule: %v", err)
	}
}

// The release pipeline reads one level of the record directory, so a record in a
// subdirectory must not satisfy the rule.
func TestEnforceRejectsANestedRecord(t *testing.T) {
	_, err := Enforce(Snapshot{
		Dir:   ".changes",
		Known: []string{"demo"},
		Changes: []Change{
			{Path: ".changes/sub/index.md", Status: StatusAdded},
			{Path: "src/app.js", Status: StatusModified},
		},
		Read: staticRead(validRecord),
	})
	var missing *MissingRecordError
	if !errors.As(err, &missing) {
		t.Fatalf("a nested record must not satisfy the rule, got %v", err)
	}
}

// The scope decides which changes need a record: a file outside it does not, and
// a file inside it does, while the record itself always sits outside both.
func TestEnforceRespectsTheConfiguredScope(t *testing.T) {
	snapshot := func(files ...Change) Snapshot {
		return Snapshot{Dir: ".changes", Scope: []string{"src", "cmd"}, Known: []string{"demo"}, Changes: files, Read: staticRead(validRecord)}
	}

	docsOnly := snapshot(Change{Path: "docs/guide.md", Status: StatusModified}, Change{Path: ".changes/index.md", Status: StatusAdded})
	if _, err := Enforce(docsOnly); err != nil {
		t.Fatalf("a change outside the scope needs no record: %v", err)
	}

	scoped := snapshot(Change{Path: "src/app.js", Status: StatusModified})
	var missing *MissingRecordError
	if _, err := Enforce(scoped); !errors.As(err, &missing) {
		t.Fatalf("a change inside the scope needs a record, got %v", err)
	}

	covered := snapshot(Change{Path: "cmd/main.go", Status: StatusAdded}, Change{Path: ".changes/index.md", Status: StatusAdded})
	if _, err := Enforce(covered); err != nil {
		t.Fatalf("a scoped change with a record passes: %v", err)
	}
}

func TestEnforceRejectsAnInvalidRecord(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record string
	}{
		{"unparseable", brokenRecord},
		{"unregistered package", unknownPackageRecord},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Enforce(Snapshot{
				Dir:   ".changes",
				Known: []string{"demo"},
				Changes: []Change{
					{Path: ".changes/index.md", Status: StatusAdded},
					{Path: "src/app.js", Status: StatusModified},
				},
				Read: staticRead(tc.record),
			})
			var invalid *InvalidRecordError
			if !errors.As(err, &invalid) {
				t.Fatalf("an invalid record must be rejected, got %v", err)
			}
			if invalid.Path != ".changes/index.md" {
				t.Fatalf("InvalidRecordError.Path = %q", invalid.Path)
			}
		})
	}
}

func TestTouchesScope(t *testing.T) {
	ignore := []string{".quailegg.toml", ".changes/README.md"}
	if !TouchesScope([]string{"cmd/main.go"}, ".changes", []string{"cmd"}, ignore) {
		t.Fatal("a scoped file must count as a scope change")
	}
	if TouchesScope([]string{".changes/x.md", ".quailegg.toml"}, ".changes", []string{"cmd"}, ignore) {
		t.Fatal("records and managed files must not count as scope changes")
	}
}
