package cli

import (
	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/tui"
)

// errInitAborted reports that the user cancelled the init wizard.
var errInitAborted = tui.ErrAborted

func runCommitWizard(a *app.App) (app.CommitOptions, error) {
	draft, err := tui.RunCommit(a.Cfg.PackageNames(), tui.Draft{})
	if err != nil {
		return app.CommitOptions{}, err
	}
	return app.CommitOptions{
		Message:  draft.Message,
		Type:     draft.Type,
		Scope:    draft.Scope,
		Body:     draft.Body,
		Packages: draft.Packages,
	}, nil
}

func maybeInitWizard(cwd string, opts *app.InitOptions) error {
	root, err := fsx.RepoRoot(cwd)
	if err != nil {
		return err
	}
	detected, err := registry.DetectAll(root)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(detected))
	for _, pkg := range detected {
		names = append(names, pkg.Name)
	}
	mode := opts.Mode
	if mode == "" {
		mode = config.ModePR
	}
	choices, err := tui.RunInit(tui.InitChoices{
		Mode:          mode,
		WithHook:      opts.WithHook,
		WithWorkflows: opts.WithWorkflows,
		WithStatus:    opts.WithStatus,
		Packages:      names,
	})
	if err != nil {
		return err
	}
	opts.Mode = choices.Mode
	opts.WithHook = choices.WithHook
	opts.WithWorkflows = choices.WithWorkflows
	opts.WithStatus = choices.WithStatus
	return nil
}
