// Package ui owns all human-facing output.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Printer writes human-readable or JSON output.
type Printer struct {
	Out   io.Writer
	Err   io.Writer
	Color bool
	// Verbose enables extra lines.
	Verbose bool
	// JSON mode prints machine-readable output instead of human text.
	JSON bool
}

// New builds a printer bound to the process streams.
func New() *Printer {
	return &Printer{
		Out:   os.Stdout,
		Err:   os.Stderr,
		Color: isTerminal(os.Stdout),
	}
}

// Discard builds a printer that drops everything; used by tests.
func Discard() *Printer {
	return &Printer{Out: io.Discard, Err: io.Discard}
}

func (p *Printer) paint(code, text string) string {
	if !p.Color || p.JSON {
		return text
	}
	return code + text + "\x1b[0m"
}

// Info prints a neutral line.
func (p *Printer) Info(format string, args ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintln(p.Out, fmt.Sprintf(format, args...))
}

// Verbosef prints only when verbose output is enabled.
func (p *Printer) Verbosef(format string, args ...any) {
	if !p.Verbose || p.JSON {
		return
	}
	fmt.Fprintln(p.Out, fmt.Sprintf(format, args...))
}

// Success prints a success line.
func (p *Printer) Success(format string, args ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintln(p.Out, p.paint("\x1b[32m", fmt.Sprintf(format, args...)))
}

// Warn prints a warning to stderr.
func (p *Printer) Warn(format string, args ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintln(p.Err, p.paint("\x1b[33m", "warning: "+fmt.Sprintf(format, args...)))
}

// Errorf prints an error to stderr.
func (p *Printer) Errorf(format string, args ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintln(p.Err, p.paint("\x1b[31m", "error: "+fmt.Sprintf(format, args...)))
}

// EmitJSON prints v as indented JSON.
func (p *Printer) EmitJSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Title renders a section title.
func (p *Printer) Title(text string) {
	if p.JSON {
		return
	}
	fmt.Fprintln(p.Out, p.paint("\x1b[1m", text))
}

// Indent indents a block of text by n spaces.
func Indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}
