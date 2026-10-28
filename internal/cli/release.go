package cli

import (
	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/tui"
)

func releaseOptions(cmd *cobra.Command) release.Options {
	var opts release.Options
	opts.AllowDirty, _ = cmd.Flags().GetBool("allow-dirty")
	opts.SkipVerify, _ = cmd.Flags().GetBool("skip-verify")
	opts.NoPush, _ = cmd.Flags().GetBool("no-push")
	opts.NoGitHubRelease, _ = cmd.Flags().GetBool("no-github-release")
	opts.NoPublish, _ = cmd.Flags().GetBool("no-publish")
	opts.NoAssets, _ = cmd.Flags().GetBool("no-assets")
	return opts
}

func addReleaseFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("allow-dirty", false, i18n.T("flag.allow_dirty"))
	cmd.Flags().Bool("skip-verify", false, i18n.T("flag.skip_verify"))
	cmd.Flags().Bool("no-push", false, i18n.T("flag.no_push"))
	cmd.Flags().Bool("no-github-release", false, i18n.T("flag.no_github_release"))
	cmd.Flags().Bool("no-publish", false, i18n.T("flag.no_publish"))
	cmd.Flags().Bool("no-assets", false, i18n.T("flag.no_assets"))
}

func newReleaseCommand(rt *Runtime) *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "release",
		Short: i18n.T("cmd.release.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := rt.App()
			if err != nil {
				return err
			}
			opts := releaseOptions(cmd)
			if dryRun {
				plan, err := a.Plan(cmd.Context())
				if err != nil {
					return fail(3, err)
				}
				printPlan(rt.Printer(), plan)
				return nil
			}
			if !yes && rt.Printer().Color {
				preview, err := a.Plan(cmd.Context())
				if err != nil {
					return fail(3, err)
				}
				if preview.Phase != release.PhaseNone {
					body := i18n.Tf("tui.release.body", preview.Phase, preview.Version.Current, preview.Version.Next, preview.Tag)
					ok, err := tui.Confirm(i18n.T("tui.release.title"), body)
					if err != nil {
						return fail(1, err)
					}
					if !ok {
						return &ExitError{Code: 0}
					}
				}
			}
			plan, err := a.Release(cmd.Context(), opts)
			if err != nil {
				return fail(1, err)
			}
			if plan.Phase != release.PhaseNone {
				a.UI.Success("%s", i18n.Tf("release.done", plan.Version.Next))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("flag.dry_run"))
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("flag.yes"))
	addReleaseFlags(cmd)
	return cmd
}
