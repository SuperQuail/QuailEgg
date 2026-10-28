package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newVersionCommand(rt *Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: i18n.T("cmd.version.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), rt.BuildVersion())
			return nil
		},
	}
}
