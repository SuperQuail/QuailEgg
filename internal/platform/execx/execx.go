// Package execx runs external commands through an injectable interface.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Result is the captured result of one command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes commands. Tests substitute a fake implementation.
type Runner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (Result, error)
}

// OSRunner executes real processes.
type OSRunner struct{}

// Run implements Runner.
func (OSRunner) Run(ctx context.Context, dir, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errOut.String()}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		return res, &Error{Name: name, Args: append([]string(nil), args...), Result: res, Err: err}
	}
	return res, nil
}

// Error describes a failed command with its captured output.
type Error struct {
	Name   string
	Args   []string
	Result Result
	Err    error
}

func (e *Error) Error() string {
	line := strings.TrimSpace(e.Result.Stderr)
	if line == "" {
		line = strings.TrimSpace(e.Result.Stdout)
	}
	if line == "" {
		return fmt.Sprintf("%s %s: %v", e.Name, strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("%s %s: %s", e.Name, strings.Join(e.Args, " "), firstLine(line))
}

// Unwrap exposes the underlying error.
func (e *Error) Unwrap() error { return e.Err }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// LookPath reports whether an executable is available on PATH.
func LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
