package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newChangelogCommand(rt *Runtime) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "changelog",
		Short: i18n.T("cmd.changelog.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			if err := a.Changelog(cmd.Context(), dryRun); err != nil {
				return fail(3, err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("flag.dry_run"))
	return cmd
}
