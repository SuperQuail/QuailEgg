package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/scaffold"
)

func newInitCommand(rt *Runtime) *cobra.Command {
	var opts app.InitOptions
	var jsonOut, dryRun, force, noHook, noWorkflows, noStatus, noAddPackages, noInput, develop bool
	var qeggVersion string
	var packages, exclude []string
	var depth int
	cmd := &cobra.Command{
		Use:   "init",
		Short: i18n.T("cmd.init.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := rt.Workdir()
			if err != nil {
				return fail(2, err)
			}
			opts.WithHook = !noHook
			opts.WithWorkflows = !noWorkflows
			opts.WithStatus = !noStatus
			opts.AddPackages = !noAddPackages
			opts.Force = force
			opts.DryRun = dryRun
			opts.ScanDepth = depth
			opts.Exclude = exclude
			if cmd.Flags().Changed("packages") && noAddPackages {
				return fail(2, errors.New(i18n.T("init.err.packages_no_add")))
			}
			root, err := fsx.RepoRoot(cwd)
			if err != nil {
				return fail(2, errors.New(i18n.T("error.not_git_repo")))
			}
			if depth < 0 {
				return fail(2, errors.New(i18n.T("scan.err.depth")))
			}
			scan, err := app.Scan(cmd.Context(), root, app.ScanOptions{Depth: depth, Exclude: exclude, Remote: opts.Remote})
			if err != nil {
				return fail(2, err)
			}
			if cmd.Flags().Changed("packages") {
				ids, err := app.SelectPackages(scan, packages)
				if err != nil {
					return fail(2, err)
				}
				opts.Packages = ids
			}
			if !noInput && !dryRun && rt.Printer().Color {
				if err := maybeInitWizard(scan, &opts); err != nil {
					if errors.Is(err, errInitAborted) {
						return &ExitError{Code: 0}
					}
					return fail(2, err)
				}
			}
			if cmd.Flags().Changed("develop") {
				value := develop
				opts.Develop = &value
			}
			if qeggVersion != "" {
				normalized, err := normalizeQeggVersion(qeggVersion)
				if err != nil {
					return fail(2, err)
				}
				opts.QeggVersion = normalized
			}
			if opts.QeggVersion == "" {
				opts.QeggVersion = rt.InstallVersion()
			}
			opts.Candidates = scan
			p := rt.Printer()
			p.JSON = jsonOut
			plan, err := app.Init(cmd.Context(), cwd, p, opts)
			if err != nil {
				return fail(3, err)
			}
			if jsonOut {
				return p.EmitJSON(planSummary(plan, dryRun))
			}
			printInitPlan(p, plan, dryRun)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noInput, "no-input", false, i18n.T("flag.no_input"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("flag.dry_run"))
	cmd.Flags().BoolVar(&jsonOut, "json", false, i18n.T("flag.json"))
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("flag.force"))
	cmd.Flags().StringVar(&opts.Mode, "mode", "", i18n.T("flag.mode"))
	cmd.Flags().StringVar(&opts.Branch, "branch", "", i18n.T("flag.branch"))
	cmd.Flags().StringVar(&opts.TagPrefix, "tag-prefix", "", i18n.T("flag.tag_prefix"))
	cmd.Flags().StringVar(&opts.Remote, "remote", "", i18n.T("flag.remote"))
	cmd.Flags().StringSliceVar(&opts.Registries, "registries", nil, i18n.T("flag.registries"))
	cmd.Flags().StringVar(&opts.ChangesDir, "changes-dir", "", i18n.T("flag.changes_dir"))
	cmd.Flags().StringVar(&opts.Environment, "environment", "", i18n.T("flag.environment"))
	cmd.Flags().StringSliceVar(&opts.Scope, "scope", nil, i18n.T("flag.scope"))
	cmd.Flags().StringSliceVar(&opts.VerifyCommands, "verify-cmd", nil, i18n.T("flag.verify_cmd"))
	cmd.Flags().BoolVar(&opts.NoVerify, "no-verify-cmd", false, i18n.T("flag.no_verify_cmd"))
	cmd.Flags().BoolVar(&noHook, "no-hook", false, i18n.T("flag.no_hook"))
	cmd.Flags().BoolVar(&noWorkflows, "no-workflows", false, i18n.T("flag.no_workflows"))
	cmd.Flags().BoolVar(&noStatus, "no-status-workflow", false, i18n.T("flag.no_status_workflow"))
	cmd.Flags().BoolVar(&noAddPackages, "no-add-packages", false, i18n.T("flag.no_add_packages"))
	cmd.Flags().StringSliceVar(&packages, "packages", nil, i18n.T("flag.init_packages"))
	cmd.Flags().IntVar(&depth, "depth", app.DefaultScanDepth, i18n.T("flag.scan_depth"))
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, i18n.T("flag.scan_exclude"))
	cmd.Flags().StringVar(&qeggVersion, "qegg-version", "", i18n.T("flag.qegg_version"))
	cmd.Flags().BoolVar(&develop, "develop", false, i18n.T("flag.develop"))
	return cmd
}

// normalizeQeggVersion accepts "latest" or a released tag, so bootstrapping can
// pin a version even when qegg runs from a source build.
func normalizeQeggVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "latest" {
		return "latest", nil
	}
	version := strings.TrimPrefix(value, "v")
	if !releaseVersionPattern.MatchString(version) {
		return "", fmt.Errorf(i18n.T("error.qegg_version"), value)
	}
	return "v" + version, nil
}

func printInitPlan(p *ui.Printer, plan *scaffold.Plan, dryRun bool) {
	for _, file := range plan.Files {
		switch file.Action {
		case scaffold.ActionSkip:
			p.Info("%s", i18n.Tf("init.skip", file.Path, file.Reason))
		case scaffold.ActionUpdate:
			p.Info("%s", i18n.Tf("init.update", file.Path))
		default:
			if dryRun {
				p.Info("%s", i18n.Tf("init.would_write", file.Path))
			} else {
				p.Info("%s", i18n.Tf("init.write", file.Path))
			}
		}
	}
	if len(plan.Added) > 0 {
		p.Success("%s", i18n.Tf("init.registered", len(plan.Added), strings.Join(plan.Added, ", ")))
	}
	for _, warning := range plan.Warnings {
		p.Warn("%s", warning)
	}
	if !dryRun {
		p.Success("%s", i18n.T("init.done"))
	}
}

// InitSummary is the JSON shape of qegg init.
type InitSummary struct {
	SchemaVersion int        `json:"schema_version"`
	DryRun        bool       `json:"dry_run"`
	AddedPackages []string   `json:"added_packages"`
	KeptPackages  []string   `json:"kept_packages"`
	Files         []InitFile `json:"files"`
	Warnings      []string   `json:"warnings"`
}

// InitFile is one planned file in the JSON summary.
type InitFile struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

func planSummary(plan *scaffold.Plan, dryRun bool) InitSummary {
	summary := InitSummary{
		SchemaVersion: 1,
		DryRun:        dryRun,
		AddedPackages: plan.Added,
		KeptPackages:  plan.Kept,
		Warnings:      plan.Warnings,
	}
	for _, file := range plan.Files {
		summary.Files = append(summary.Files, InitFile{Path: file.Path, Action: string(file.Action), Reason: file.Reason})
	}
	return summary
}
