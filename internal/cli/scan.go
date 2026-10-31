package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func newScanCommand(rt *Runtime) *cobra.Command {
	var jsonOut bool
	var depth int
	var exclude []string
	var remote string
	cmd := &cobra.Command{
		Use:   "scan",
		Short: i18n.T("cmd.scan.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := rt.Workdir()
			if err != nil {
				return fail(2, err)
			}
			root, err := fsx.RepoRoot(cwd)
			if err != nil {
				return fail(2, errors.New(i18n.T("error.not_git_repo")))
			}
			if depth < 0 {
				return fail(2, errors.New(i18n.T("scan.err.depth")))
			}
			result, err := app.Scan(cmd.Context(), root, app.ScanOptions{Depth: depth, Exclude: exclude, Remote: remote})
			if err != nil {
				return fail(1, err)
			}
			p := rt.Printer()
			p.JSON = jsonOut
			if jsonOut {
				if err := p.EmitJSON(result); err != nil {
					return fail(1, err)
				}
				return nil
			}
			printScan(p, result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, i18n.T("flag.json"))
	cmd.Flags().IntVar(&depth, "depth", app.DefaultScanDepth, i18n.T("flag.scan_depth"))
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, i18n.T("flag.scan_exclude"))
	cmd.Flags().StringVar(&remote, "remote", "", i18n.T("flag.remote"))
	return cmd
}

// printScan renders the human readable scan report.
func printScan(p *ui.Printer, result *app.ScanResult) {
	p.Info("%s", i18n.Tf("scan.summary", result.Root, result.Depth, len(result.Packages)))
	for _, pkg := range result.Packages {
		label := i18n.T("scan.label_new")
		if pkg.Registered {
			label = i18n.T("scan.label_registered")
		} else if !pkg.Selectable {
			label = i18n.T("scan.label_skipped")
		}
		display := pkg.Key
		if display == "" {
			display = pkg.DisplayName()
		}
		p.Info("%s", i18n.Tf("scan.package_line", label, display, pkg.Resolver, pkg.Path, displayVersion(pkg.Version)))
		if !pkg.Selectable && !pkg.Registered {
			p.Info("%s", i18n.Tf("scan.reason_line", pkg.ReasonText()))
		}
	}
	p.Info("%s", i18n.Tf("scan.scope", result.VersionScope))
	for _, file := range result.VersionFiles {
		p.Info("%s", i18n.Tf("scan.version_file", file.Path, displayVersion(file.Version)))
	}
	for _, warning := range result.Warnings {
		p.Warn("%s", warning)
	}
}

// displayVersion renders an empty version placeholder.
func displayVersion(version string) string {
	if version == "" {
		return "-"
	}
	return version
}
