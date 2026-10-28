package scaffold

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"text/template"
)

// Template action delimiters. The generated GitHub workflows contain Actions
// expressions such as ${{ secrets.GITHUB_TOKEN }}; the default {{ }} pair
// would be parsed as Go template actions, so scaffold templates use [[ ]]
// and keep GitHub expressions literal.
const (
	openAction  = "[["
	closeAction = "]]"
)

// renderer executes scaffold assets. It is a value type with an injected
// filesystem so tests can render from an in-memory tree.
type renderer struct {
	fs fs.FS
}

// assets is the renderer for the embedded scaffold assets, rooted at assets/.
var assets = renderer{fs: subtree(assetFS, "assets")}

// subtree roots fsys at dir. The embedded asset tree cannot be missing, so a
// failure here is a programming error rather than a runtime condition.
func subtree(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(fmt.Sprintf("scaffold: embedded assets: %v", err))
	}
	return sub
}

// render reads name from the asset filesystem and executes it.
func (r renderer) render(name string, data any) ([]byte, error) {
	src, err := fs.ReadFile(r.fs, name)
	if err != nil {
		return nil, fmt.Errorf("scaffold: read asset %s: %w", name, err)
	}
	return r.execute(name, string(src), data)
}

// eval executes a template source string, used for destination paths.
func (r renderer) eval(name, src string, data any) (string, error) {
	out, err := r.execute(name, src, data)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (r renderer) execute(name, src string, data any) ([]byte, error) {
	tmpl, err := template.New(name).
		Delims(openAction, closeAction).
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"indent":  indent,
			"shquote": shquote,
		}).
		Parse(src)
	if err != nil {
		return nil, fmt.Errorf("scaffold: parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("scaffold: render %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// indent prefixes every line of s with n spaces. An empty value stays empty,
// so a conditional block disappears instead of leaving a blank line.
func indent(n int, s string) string {
	if s == "" {
		return ""
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

// shquote renders s as a POSIX shell single-quoted word.
func shquote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
