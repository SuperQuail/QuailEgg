package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newResumeCommand(rt *Runtime) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "resume",
		Short: i18n.T("cmd.resume.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			if _, err := a.Resume(cmd.Context(), releaseOptions(cmd)); err != nil {
				return fail(1, err)
			}
			_ = yes
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("flag.yes"))
	addReleaseFlags(cmd)
	return cmd
}
