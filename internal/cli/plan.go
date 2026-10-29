package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/changelog"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release"
)

func newPlanCommand(rt *Runtime) *cobra.Command {
	var jsonOut, markdown bool
	cmd := &cobra.Command{
		Use:   "plan",
		Short: i18n.T("cmd.plan.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			plan, err := a.Plan(cmd.Context())
			if err != nil {
				return fail(3, err)
			}
			p := rt.Printer()
			if jsonOut {
				p.JSON = true
				return p.EmitJSON(plan)
			}
			if markdown {
				section := release.ChangelogSection(plan, a.Now())
				_, err := io.WriteString(cmd.OutOrStdout(), changelog.Render(section))
				return err
			}
			printPlan(p, plan)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, i18n.T("flag.json"))
	cmd.Flags().BoolVar(&markdown, "markdown", false, i18n.T("flag.markdown"))
	return cmd
}

func printPlan(p *ui.Printer, plan *release.Plan) {
	if plan.Channel != "" {
		p.Info("%s", i18n.Tf("plan.channel", plan.Channel))
	}
	if plan.Graduating {
		p.Info("%s", i18n.T("plan.graduating"))
	}
	switch plan.Phase {
	case release.PhaseVersion:
		p.Info("%s", i18n.Tf("plan.phase_version", plan.Version.Current, plan.Version.Next, plan.Version.Bump))
	case release.PhasePublish:
		p.Info("%s", i18n.Tf("plan.phase_publish", plan.Version.Next))
	default:
		p.Info("%s", i18n.T("plan.nothing"))
		return
	}
	p.Info("%s", i18n.Tf("plan.tag", plan.Tag))
	if plan.Assets != nil {
		p.Info("%s", i18n.Tf("plan.assets_line", plan.Assets.Name, len(plan.Assets.Targets)))
	}
	for _, change := range plan.Changes {
		p.Info("%s", i18n.Tf("plan.change_line", change.Type, change.Body))
	}
	for _, step := range plan.Steps {
		p.Info("%s", i18n.Tf("plan.step_line", step.ID, step.Title))
	}
	for _, warning := range plan.Warnings {
		p.Warn("%s", warning)
	}
}
