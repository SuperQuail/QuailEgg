package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newPreCommand(rt *Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "pre [alpha|beta|rc|exit]",
		Short: i18n.T("cmd.pre.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			p := rt.Printer()
			value := ""
			if len(args) > 0 {
				value = strings.TrimSpace(args[0])
			}
			opts := app.PreOptions{}
			switch value {
			case "":
			case "exit":
				opts.Exit = true
			default:
				opts.Channel = value
			}
			report, err := a.Pre(cmd.Context(), opts)
			if err != nil {
				return fail(2, err)
			}
			switch {
			case opts.Exit:
				p.Success("%s", i18n.Tf("pre.exit", report.Next))
			case opts.Channel != "":
				p.Success("%s", i18n.Tf("pre.enter", report.Channel))
				if report.Next != "" {
					p.Info("%s", i18n.Tf("pre.next", report.Next))
				}
				p.Info("%s", i18n.T("pre.commit_hint"))
			default:
				if report.Channel == "" {
					p.Info("%s", i18n.T("pre.none"))
				} else {
					p.Info("%s", i18n.Tf("pre.active", report.Channel))
				}
				if report.Next != "" {
					p.Info("%s", i18n.Tf("pre.next", report.Next))
				}
			}
			return nil
		},
	}
}
