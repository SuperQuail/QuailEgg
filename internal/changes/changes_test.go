package changes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/semver"
)

func parse(t *testing.T, body string) *Record {
	t.Helper()
	rec, err := Parse("test.md", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return rec
}

func TestParseStandard(t *testing.T) {
	rec := parse(t, "---\ntype: feat\nscope: cache\npackages:\n  pkg: minor\n---\n\nAdd cache ttl.\n")
	if rec.Type != "feat" || rec.Scope != "cache" || rec.Body != "Add cache ttl." {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.Packages["pkg"] != semver.Minor {
		t.Fatalf("unexpected packages: %+v", rec.Packages)
	}
	if !rec.Release {
		t.Fatal("release should default to true")
	}
}

func TestParseWithoutOpeningSeparator(t *testing.T) {
	rec := parse(t, "type: fix\npackages:\n  pkg: patch\n---\nFixed it.\n")
	if rec.Type != "fix" || rec.Body != "Fixed it." {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

func TestParseBOMAndCRLF(t *testing.T) {
	raw := "\xef\xbb\xbf---\r\ntype: perf\r\npackages:\r\n  pkg: patch\r\n---\r\n\r\nFaster.\r\n"
	rec := parse(t, raw)
	if rec.Type != "perf" || rec.Body != "Faster." {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

func TestParseUnknownField(t *testing.T) {
	_, err := Parse("test.md", strings.NewReader("---\ntype: feat\npackages:\n  pkg: patch\nunknown: 1\n---\nbody\n"))
	if err == nil {
		t.Fatal("expected an error for unknown front matter fields")
	}
}

func TestValidate(t *testing.T) {
	rec := parse(t, "---\ntype: feat\npackages:\n  pkg: minor\n---\nbody\n")
	if err := rec.Validate([]string{"pkg"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := rec.Validate([]string{"other"}); err == nil {
		t.Fatal("expected an error for an unregistered package")
	}
	rec.Path = "/repo/.changes/Not_A_Slug.md"
	if err := rec.Validate([]string{"pkg"}); err == nil {
		t.Fatal("expected an error for an invalid slug")
	}
}

func TestRenderRoundTrip(t *testing.T) {
	rec := &Record{
		Type:     "feat",
		Scope:    "cache",
		Packages: map[string]semver.Bump{"pkg": semver.Minor, "other": semver.Patch},
		Release:  true,
		Body:     "Add cache ttl.",
	}
	data, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	parsed, err := Parse("test.md", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse rendered: %v\n%s", err, data)
	}
	if parsed.Body != rec.Body || parsed.Type != rec.Type || len(parsed.Packages) != 2 {
		t.Fatalf("round trip mismatch: %+v", parsed)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Support cache TTL":       "support-cache-ttl.md",
		"feat(cache): new option": "feat-cache-new-option.md",
		"???":                     "change.md",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadDirSkipsReadme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b-change.md"), []byte("---\ntype: fix\npackages:\n  pkg: patch\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(records) != 1 || !strings.HasSuffix(records[0].Path, "b-change.md") {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestValidateSlugWordLimitMessage(t *testing.T) {
	rec := parse(t, "---\ntype: feat\npackages:\n  pkg: minor\n---\nbody\n")
	rec.Path = "/repo/.changes/add-a-greeting-command.md"
	err := rec.Validate([]string{"pkg"})
	if err == nil {
		t.Fatal("expected an error for a four word slug")
	}
	if !strings.Contains(err.Error(), "three words") {
		t.Fatalf("error %q should state the real word limit", err)
	}
}
