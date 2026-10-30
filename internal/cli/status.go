package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func newStatusCommand(rt *Runtime) *cobra.Command {
	var jsonOut, offline bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: i18n.T("cmd.status.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			report, err := a.Status(cmd.Context(), offline)
			if err != nil {
				return fail(1, err)
			}
			p := rt.Printer()
			if jsonOut {
				p.JSON = true
				return p.EmitJSON(report)
			}
			printStatus(p, report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, i18n.T("flag.json"))
	cmd.Flags().BoolVar(&offline, "offline", false, i18n.T("flag.offline"))
	return cmd
}

func printStatus(p *ui.Printer, report *app.StatusReport) {
	if report.Marker != nil {
		p.Info("%s", i18n.Tf("status.marker", report.Marker.Version, report.Marker.Mode))
	} else {
		p.Info("%s", i18n.T("status.no_marker"))
	}
	if len(report.Packages) > 1 {
		for _, pkg := range report.Packages {
			p.Info("%s", i18n.Tf("status.package_line", pkg.Name, pkg.Tag, pkg.TagExists, pkg.Published))
		}
	} else {
		p.Info("%s", i18n.Tf("status.tag", report.Tag, report.TagExists, report.Published))
	}
	if report.Channel != "" {
		p.Info("%s", i18n.Tf("status.channel", report.Channel))
	}
	if len(report.Pending) > 0 {
		p.Info("%s", i18n.Tf("status.pending", len(report.Pending)))
	}
	if report.LastRun != nil {
		p.Info("%s", i18n.Tf("status.last_run", report.LastRun.RunID, report.LastRun.Phase, report.LastRun.Status))
	}
}
