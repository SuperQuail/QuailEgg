package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newHookCommand(rt *Runtime) *cobra.Command {
	hook := &cobra.Command{
		Use:    "hook",
		Short:  i18n.T("cmd.hook.short"),
		Hidden: true,
	}
	hook.AddCommand(&cobra.Command{
		Use:   "pre-commit",
		Short: i18n.T("cmd.hook.pre_commit.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			if err := a.HookPreCommit(cmd.Context()); err != nil {
				return fail(3, err)
			}
			return nil
		},
	})
	return hook
}
