// Package cli wires the process entry point to the subcommands.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// Execute runs the CLI and returns a process exit code.
func Execute(args []string) int {
	if err := i18n.Init(i18n.DefaultLanguage); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	lang, err := resolveLanguage(prescanLanguage(args))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		return 2
	}
	if err := i18n.Init(lang); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		return 2
	}

	root := newRootCommand()
	root.SetArgs(args)
	execErr := root.Execute()
	if execErr == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(execErr, &exitErr) {
		if exitErr.Err != nil {
			fmt.Fprintln(os.Stderr, "error: "+exitErr.Err.Error())
		}
		return exitErr.Code
	}
	fmt.Fprintln(os.Stderr, "error: "+execErr.Error())
	return 1
}

func prescanLanguage(args []string) string {
	for index, arg := range args {
		if arg == "--lang" && index+1 < len(args) {
			return args[index+1]
		}
		if strings.HasPrefix(arg, "--lang=") {
			return strings.TrimPrefix(arg, "--lang=")
		}
	}
	return ""
}

// resolveLanguage picks the interface language. It is user-scoped: the flag
// wins, then the environment, then the per-user preference file; a repository
// configuration never selects the language.
func resolveLanguage(flagValue string) (string, error) {
	if flagValue != "" {
		if !i18n.SupportedLang(flagValue) {
			return "", fmt.Errorf(i18n.T("lang.invalid"), flagValue, strings.Join(i18n.Supported(), ", "))
		}
		return i18n.Normalize(flagValue), nil
	}
	if env := os.Getenv("QEGG_LANG"); env != "" {
		if !i18n.SupportedLang(env) {
			return "", fmt.Errorf(i18n.T("lang.invalid"), env, strings.Join(i18n.Supported(), ", "))
		}
		return i18n.Normalize(env), nil
	}
	if user := i18n.LoadUserLanguage(); user != "" && i18n.SupportedLang(user) {
		return i18n.Normalize(user), nil
	}
	return i18n.DefaultLanguage, nil
}

func newRootCommand() *cobra.Command {
	opts := &Options{}
	rt := newRuntime(opts)
	root := &cobra.Command{
		Use:           "qegg",
		Short:         i18n.T("cmd.root.short"),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&opts.ConfigPath, "config", "", i18n.T("flag.config"))
	root.PersistentFlags().StringVar(&opts.CWD, "cwd", "", i18n.T("flag.cwd"))
	root.PersistentFlags().BoolVar(&opts.Verbose, "verbose", false, i18n.T("flag.verbose"))
	root.PersistentFlags().BoolVar(&opts.NoColor, "no-color", false, i18n.T("flag.no_color"))
	root.PersistentFlags().StringVar(&opts.Lang, "lang", "", i18n.T("flag.lang"))
	register(root, rt)
	root.Version = rt.BuildVersion()
	return root
}
