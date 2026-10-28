package scaffold

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRendererKeepsGitHubExpressions(t *testing.T) {
	src := "on:\n  push:\n    branches: [main]\n    token: ${{ secrets.GITHUB_TOKEN }}\nbranch: [[ .Branch ]]\n"
	r := renderer{fs: fstest.MapFS{
		"workflow.yml": {Data: []byte(src)},
	}}
	out, err := r.render("workflow.yml", map[string]any{"Branch": "release/1.x"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := string(out)
	for _, want := range []string{"${{ secrets.GITHUB_TOKEN }}", "branch: release/1.x"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered template missing %q:\n%s", want, got)
		}
	}
}

func TestRendererEscapesTemplateDelimiters(t *testing.T) {
	r := renderer{fs: fstest.MapFS{
		"literal.tmpl": {Data: []byte("value: [[ \"[[\" ]]\n")},
	}}
	out, err := r.render("literal.tmpl", map[string]any{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got, want := string(out), "value: [[\n"; got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
}

func TestRendererReportsBrokenTemplates(t *testing.T) {
	r := renderer{fs: fstest.MapFS{
		"broken.tmpl": {Data: []byte("value: [[ .Foo \n")},
		"strict.tmpl": {Data: []byte("value: [[ .Missing ]]\n")},
	}}
	if _, err := r.render("absent.tmpl", map[string]any{}); err == nil {
		t.Error("expected an error for a missing asset")
	}
	if _, err := r.render("broken.tmpl", map[string]any{}); err == nil {
		t.Error("expected an error for a malformed template")
	}
	if _, err := r.render("strict.tmpl", map[string]any{}); err == nil {
		t.Error("expected an error for a missing field")
	}
}

func TestRendererRendersDestinations(t *testing.T) {
	r := renderer{fs: fstest.MapFS{}}
	got, err := r.eval("destination", "[[ .ChangesDir ]]/template.md", map[string]any{"ChangesDir": ".changes"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if want := ".changes/template.md"; got != want {
		t.Fatalf("eval returned %q, want %q", got, want)
	}
}

func TestIndent(t *testing.T) {
	cases := []struct {
		name string
		n    int
		in   string
		want string
	}{
		{name: "single line", n: 2, in: "a", want: "  a"},
		{name: "multiple lines", n: 6, in: "- uses: x\n  with: y", want: "      - uses: x\n        with: y"},
		{name: "empty value", n: 4, in: "", want: ""},
		{name: "trailing newline", n: 2, in: "a\nb\n", want: "  a\n  b"},
		{name: "zero indent", n: 0, in: "a\nb", want: "a\nb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := indent(tc.n, tc.in); got != tc.want {
				t.Fatalf("indent(%d, %q) = %q, want %q", tc.n, tc.in, got, tc.want)
			}
		})
	}
}

func TestShquote(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "qegg: command not found", want: "'qegg: command not found'"},
		{name: "apostrophe", in: "it's", want: "'it'\\''s'"},
		{name: "empty", in: "", want: "''"},
		{name: "dollar", in: "$HOME", want: "'$HOME'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shquote(tc.in); got != tc.want {
				t.Fatalf("shquote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
