package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newVerifyCommand(rt *Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: i18n.T("cmd.verify.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			if err := a.Verify(cmd.Context()); err != nil {
				return fail(3, err)
			}
			return nil
		},
	}
}
