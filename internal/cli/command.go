package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/buildinfo"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

// Options holds the global flag values bound by the root command.
type Options struct {
	ConfigPath string
	CWD        string
	Verbose    bool
	NoColor    bool
	Lang       string
}

// Runtime is the CLI context every subcommand receives: it owns the global
// flags and builds the printer and the application on demand.
type Runtime struct {
	opts *Options
}

func newRuntime(opts *Options) *Runtime { return &Runtime{opts: opts} }

// Printer returns a printer configured from the global flags.
func (r *Runtime) Printer() *ui.Printer {
	p := ui.New()
	p.Verbose = r.opts.Verbose
	if r.opts.NoColor {
		p.Color = false
	}
	return p
}

// Workdir resolves the effective working directory.
func (r *Runtime) Workdir() (string, error) {
	if r.opts.CWD == "" {
		return os.Getwd()
	}
	return filepath.Abs(r.opts.CWD)
}

// App loads the repository and its configuration.
func (r *Runtime) App() (*app.App, error) {
	cwd, err := r.Workdir()
	if err != nil {
		return nil, fail(2, err)
	}
	a, err := app.New(cwd, r.opts.ConfigPath, r.Printer())
	if err != nil {
		return nil, fail(2, err)
	}
	return a, nil
}

var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// BuildVersion returns the module version of the running binary.
func (r *Runtime) BuildVersion() string {
	return buildinfo.Version()
}

// InstallVersion is the qegg version pinned in generated workflows.
func (r *Runtime) InstallVersion() string {
	return buildinfo.InstallVersion()
}

// ExitError carries an explicit process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error.
func (e *ExitError) Unwrap() error { return e.Err }

func fail(code int, err error) error { return &ExitError{Code: code, Err: err} }

// register attaches every subcommand to root.
func register(root *cobra.Command, rt *Runtime) {
	root.AddCommand(
		newInitCommand(rt),
		newScanCommand(rt),
		newCheckCommand(rt),
		newCommitCommand(rt),
		newChangelogCommand(rt),
		newPlanCommand(rt),
		newVerifyCommand(rt),
		newReleaseCommand(rt),
		newResumeCommand(rt),
		newStatusCommand(rt),
		newHookCommand(rt),
		newPreCommand(rt),
		newLangCommand(rt),
		newVersionCommand(rt),
	)
}
