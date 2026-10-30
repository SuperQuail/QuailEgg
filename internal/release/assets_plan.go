package release

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/assets"
)

// planAssets resolves the files attached to the release. Configured files are
// collected from a custom build; otherwise the package ecosystem plans a build
// of its own through ecosystem.AssetPlanner.
func planAssets(d *Deps, targets []target) (*assets.Plan, []string, error) {
	cfg := d.Cfg.Release.Assets
	name := cfg.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(d.Root))
	}
	// A prebuilt directory is handed to qegg by the managed workflow.
	if d.Options.AssetsDir != "" {
		return &assets.Plan{Name: name}, nil, nil
	}
	if !cfg.Requested() {
		return nil, nil, nil
	}
	plan := &assets.Plan{Name: name, Commands: trimAll(cfg.Commands), Files: trimAll(cfg.Files)}
	var warnings []string
	if len(plan.Files) == 0 {
		if len(targets) != 1 {
			return nil, nil, errors.New(i18n.T("release.err.assets_multi_package"))
		}
		planner, ok := registry.AssetPlannerFor(targets[0].pkg.Resolver)
		if !ok {
			return nil, nil, nil
		}
		built, plannedWarnings, err := planner.PlanAssets(d.Root, targets[0].pkg, ecosystem.AssetOptions{
			Name: cfg.Name, Main: cfg.Main, Targets: cfg.Targets, Ldflags: cfg.Ldflags,
		})
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, plannedWarnings...)
		if built == nil {
			return nil, warnings, nil
		}
		plan.Name = built.Name
		plan.Main = built.Main
		plan.Targets = built.Targets
		plan.Ldflags = built.Ldflags
	}
	if err := plan.Validate(); err != nil {
		return nil, nil, err
	}
	return plan, warnings, nil
}

// trimAll trims a configured list and drops empty entries.
func trimAll(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
