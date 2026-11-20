package java

import (
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

// elementAt walks a document and returns the first element at path.
func elementAt(t *testing.T, text, path string) xmlElement {
	t.Helper()
	el, ok := findElement(text, strings.Split(path, "/")...)
	if !ok {
		t.Fatalf("no element at %s", path)
	}
	return el
}

// TestLiteralSpanClassifiesInnerContent pins the difference between a version
// element the round may rewrite and one whose content the rewrite would drop.
func TestLiteralSpanClassifiesInnerContent(t *testing.T) {
	cases := []struct {
		name   string
		inner  string
		reason ecosystem.PreflightReason
		value  string
	}{
		{"plain", "1.2.3", ecosystem.PreflightReasonNone, "1.2.3"},
		{"padded", "\n    1.2.3\n  ", ecosystem.PreflightReasonNone, "1.2.3"},
		{"comment", "<!-- keep -->1.2.3", ecosystem.PreflightReasonComment, ""},
		{"cdata", "<![CDATA[1.2.3]]>", ecosystem.PreflightReasonCDATA, ""},
		{"child", "1.2.3<extra>note</extra>", ecosystem.PreflightReasonChild, ""},
		{"processing", "<?keep me?>1.2.3", ecosystem.PreflightReasonProcessing, ""},
		{"empty", "", ecosystem.PreflightReasonEmpty, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := "<project><version>" + tc.inner + "</version></project>"
			el := elementAt(t, text, "project/version")
			start, end, reason := literalSpan(text, el)
			if reason != tc.reason {
				t.Fatalf("reason = %v, want %v", reason, tc.reason)
			}
			if reason != ecosystem.PreflightReasonNone {
				return
			}
			if got := text[start:end]; got != tc.value {
				t.Fatalf("literal = %q, want %q", got, tc.value)
			}
			if start < el.innerStart || end > el.innerEnd {
				t.Fatalf("span %d..%d escapes the element %d..%d", start, end, el.innerStart, el.innerEnd)
			}
		})
	}
}

// TestSetLiteralKeepsSurroundingBytes proves the replacement touches only the
// text run: indentation and sibling bytes stay as they were.
func TestSetLiteralKeepsSurroundingBytes(t *testing.T) {
	text := "<project>\n  <version>\n    1.2.3\n  </version> <!-- keep -->\n</project>\n"
	el := elementAt(t, text, "project/version")
	updated, err := setLiteral(text, "pom.xml", "project/version", el, "1.3.0")
	if err != nil {
		t.Fatalf("setLiteral: %v", err)
	}
	want := "<project>\n  <version>\n    1.3.0\n  </version> <!-- keep -->\n</project>\n"
	if updated != want {
		t.Fatalf("got:\n%s\nwant:\n%s", updated, want)
	}
}

// TestSetLiteralRefusesUnwritableElements covers every inner content the
// replacement would destroy, including the self-closing form that would
// otherwise render as <version/>1.3.0.
func TestSetLiteralRefusesUnwritableElements(t *testing.T) {
	for name, body := range map[string]string{
		"comment":    "<version><!-- keep -->1.2.3</version>",
		"cdata":      "<version><![CDATA[1.2.3]]></version>",
		"child":      "<version>1.2.3<extra>note</extra></version>",
		"processing": "<version><?keep me?>1.2.3</version>",
		"selfclosed": "<version/>",
		"empty":      "<version></version>",
	} {
		t.Run(name, func(t *testing.T) {
			text := "<project>" + body + "</project>"
			el := elementAt(t, text, "project/version")
			if updated, err := setLiteral(text, "pom.xml", "project/version", el, "1.3.0"); err == nil {
				t.Fatalf("setLiteral must refuse %s, got %q", name, updated)
			}
		})
	}
}
