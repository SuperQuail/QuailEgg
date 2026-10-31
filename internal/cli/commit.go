package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/semver"
	"github.com/SuperQuail/QuailEgg/internal/tui"
)

func newCommitCommand(rt *Runtime) *cobra.Command {
	var message, typ, scope, body string
	var packages []string
	var noCommit, noInput, yes bool
	cmd := &cobra.Command{
		Use:   "commit",
		Short: i18n.T("cmd.commit.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			opts := app.CommitOptions{Message: message, Type: typ, Scope: scope, Body: body, NoCommit: noCommit}
			parsed, err := parsePackages(packages, a.Cfg)
			if err != nil {
				return fail(2, err)
			}
			opts.Packages = parsed
			if len(opts.Packages) == 0 {
				if !noInput && rt.Printer().Color {
					picked, err := runCommitWizard(a)
					if err != nil {
						if errors.Is(err, tui.ErrAborted) {
							return &ExitError{Code: 0}
						}
						return fail(2, err)
					}
					opts = picked
				} else {
					return fail(2, errors.New(i18n.T("commit.missing_packages")))
				}
			}
			if _, err := a.Commit(cmd.Context(), opts); err != nil {
				return fail(3, err)
			}
			_ = yes
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", i18n.T("flag.message"))
	cmd.Flags().StringVar(&typ, "type", "", i18n.T("flag.type"))
	cmd.Flags().StringVar(&scope, "scope", "", i18n.T("flag.scope_flag"))
	cmd.Flags().StringVar(&body, "body", "", i18n.T("flag.body"))
	cmd.Flags().StringSliceVar(&packages, "packages", nil, i18n.T("flag.packages"))
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, i18n.T("flag.no_commit"))
	cmd.Flags().BoolVar(&noInput, "no-input", false, i18n.T("flag.no_input"))
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("flag.yes"))
	return cmd
}

func parsePackages(values []string, cfg *config.Config) (map[string]semver.Bump, error) {
	known := cfg.PackageNames()
	out := map[string]semver.Bump{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts := strings.FieldsFunc(value, func(r rune) bool { return r == ':' || r == '=' })
		if len(parts) != 2 {
			return nil, fmt.Errorf(i18n.T("commit.packages_format"), value)
		}
		name := strings.TrimSpace(parts[0])
		bump, err := semver.ParseBump(parts[1])
		if err != nil {
			return nil, err
		}
		key, ok := cfg.PackageKeyFor(name)
		if !ok {
			if len(known) == 1 {
				key = known[0]
			} else {
				return nil, fmt.Errorf(i18n.T("commit.unknown_package"), parts[0], strings.Join(known, ", "))
			}
		}
		out[key] = bump
	}
	return out, nil
}
