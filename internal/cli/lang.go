package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func newLangCommand(rt *Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "lang [en|zh]",
		Short: i18n.T("cmd.lang.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				fmt.Fprintln(out, i18n.Tf("lang.current", i18n.Lang()))
				fmt.Fprintln(out, i18n.Tf("lang.supported", strings.Join(i18n.Supported(), ", ")))
				return nil
			}
			lang := args[0]
			if !i18n.SupportedLang(lang) {
				return fail(2, fmt.Errorf(i18n.T("lang.invalid"), lang, strings.Join(i18n.Supported(), ", ")))
			}
			path, err := i18n.SaveUserLanguage(lang)
			if err != nil {
				return fail(1, err)
			}
			if err := i18n.Init(lang); err != nil {
				return fail(1, err)
			}
			fmt.Fprintln(out, i18n.Tf("lang.set", i18n.Normalize(lang)))
			fmt.Fprintln(out, i18n.Tf("lang.saved", path))
			return nil
		},
	}
}
