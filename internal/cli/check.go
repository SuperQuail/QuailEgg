package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func newCheckCommand(rt *Runtime) *cobra.Command {
	var jsonOut, ci bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: i18n.T("cmd.check.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			report, err := a.Check(cmd.Context(), ci)
			if err != nil {
				return fail(1, err)
			}
			p := rt.Printer()
			p.JSON = jsonOut
			if jsonOut {
				if err := p.EmitJSON(report); err != nil {
					return fail(1, err)
				}
			} else {
				printCheck(p, report)
			}
			if !report.OK {
				return &ExitError{Code: 3}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, i18n.T("flag.json"))
	cmd.Flags().BoolVar(&ci, "ci", false, i18n.T("flag.ci"))
	return cmd
}

func printCheck(p *ui.Printer, report *app.CheckReport) {
	p.Info("%s", i18n.Tf("check.summary", report.Ecosystem, report.Package, report.Version))
	for _, problem := range report.Problems {
		p.Errorf("%s: %s", problem.Code, problem.Message)
		if problem.Fix != "" {
			p.Info("%s", i18n.Tf("check.fix", problem.Fix))
		}
	}
	if report.OK {
		p.Success("%s", i18n.T("check.passed"))
	}
}
