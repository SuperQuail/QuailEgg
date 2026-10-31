package cli

import (
	"github.com/SuperQuail/QuailEgg/internal/app"
	"github.com/SuperQuail/QuailEgg/internal/config"
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

func maybeInitWizard(scan *app.ScanResult, opts *app.InitOptions) error {
	mode := opts.Mode
	if mode == "" {
		mode = config.ModePR
	}
	candidates := make([]tui.InitPackage, 0, len(scan.Packages))
	selected := map[string]bool{}
	registered := 0
	unselectable := 0
	for _, pkg := range scan.Packages {
		if pkg.Registered {
			registered++
			continue
		}
		if !pkg.Selectable {
			unselectable++
			continue
		}
		candidates = append(candidates, tui.InitPackage{
			ID:       pkg.ID,
			Key:      pkg.Key,
			Name:     pkg.DisplayName(),
			Resolver: pkg.Resolver,
			Path:     pkg.Path,
			Version:  pkg.Version,
		})
	}
	if !opts.AddPackages {
		candidates = nil
	}
	if opts.Packages != nil {
		for _, id := range opts.Packages {
			selected[id] = true
		}
	} else {
		for _, pkg := range candidates {
			selected[pkg.ID] = true
		}
	}
	choices, err := tui.RunInit(tui.InitChoices{
		Mode:          mode,
		WithHook:      opts.WithHook,
		WithWorkflows: opts.WithWorkflows,
		WithStatus:    opts.WithStatus,
		Packages:      candidates,
		Selected:      selected,
		Registered:    registered,
		Unselectable:  unselectable,
	})
	if err != nil {
		return err
	}
	opts.Mode = choices.Mode
	opts.WithHook = choices.WithHook
	opts.WithWorkflows = choices.WithWorkflows
	opts.WithStatus = choices.WithStatus
	selectedIDs := make([]string, 0, len(candidates))
	for _, pkg := range candidates {
		if choices.Selected[pkg.ID] {
			selectedIDs = append(selectedIDs, pkg.ID)
		}
	}
	opts.Packages = selectedIDs
	return nil
}
