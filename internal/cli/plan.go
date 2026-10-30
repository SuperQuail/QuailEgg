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
				var out []byte
				for _, section := range release.ChangelogSections(plan, a.Now()) {
					out = append(out, []byte(changelog.Render(section))...)
				}
				_, err := io.WriteString(cmd.OutOrStdout(), string(out))
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
		if plan.Version.Next != "" {
			p.Info("%s", i18n.Tf("plan.phase_version", plan.Version.Current, plan.Version.Next, plan.Version.Bump))
		} else {
			p.Info("%s", i18n.Tf("plan.phase_packages", len(plan.Releases)))
		}
	case release.PhasePublish:
		if plan.Version.Next != "" {
			p.Info("%s", i18n.Tf("plan.phase_publish", plan.Version.Next))
		} else {
			p.Info("%s", i18n.Tf("plan.phase_publish_packages", len(plan.Releases)))
		}
	default:
		p.Info("%s", i18n.T("plan.nothing"))
		return
	}
	for _, release := range plan.Releases {
		if release.Name == "" {
			p.Info("%s", i18n.Tf("plan.tag", release.Tag))
			continue
		}
		p.Info("%s", i18n.Tf("plan.package_line", release.Name, releaseCurrent(plan, release), release.Version, release.Tag))
	}
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

func releaseCurrent(plan *release.Plan, rel release.ReleasePlan) string {
	if len(rel.Packages) == 1 {
		for _, pkg := range plan.Packages {
			if pkg.Name == rel.Packages[0] {
				return pkg.Current
			}
		}
	}
	return plan.Version.Current
}
