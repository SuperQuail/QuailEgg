package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// StatusPackage is the publish state of one package.
type StatusPackage struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Tag       string `json:"tag"`
	TagExists bool   `json:"tag_exists"`
	Published string `json:"published"`
}

// StatusReport is the machine readable output of qegg status.
type StatusReport struct {
	SchemaVersion int              `json:"schema_version"`
	Marker        *state.Marker    `json:"marker,omitempty"`
	Tag           string           `json:"tag"`
	TagExists     bool             `json:"tag_exists"`
	Published     string           `json:"published"`
	Packages      []StatusPackage  `json:"packages,omitempty"`
	Pending       []string         `json:"pending_changes"`
	LastRun       *state.RunRecord `json:"last_run,omitempty"`
	Channel       string           `json:"channel,omitempty"`
}

// Status reports the current release state.
func (a *App) Status(ctx context.Context, offline bool) (*StatusReport, error) {
	report := &StatusReport{SchemaVersion: 1, Published: "unknown"}
	pre, err := state.LoadPreRelease(a.Root)
	if err != nil {
		return nil, err
	}
	if pre != nil {
		report.Channel = pre.Channel
	}
	rel, err := state.LoadMarker(a.Root)
	if err != nil {
		return nil, err
	}
	report.Marker = rel

	detected, err := registry.DetectAll(a.Root)
	if err != nil {
		return nil, err
	}
	scope := a.Cfg.Release.VersionScope
	if scope == "" {
		scope = config.VersionScopePackage
	}
	names := a.Cfg.PackageNames()
	if len(names) == 0 {
		for _, pkg := range detected {
			names = append(names, pkg.Name)
		}
	}
	for _, name := range names {
		pkg, problem := a.inspectPackage(detected, name)
		if problem != nil {
			return nil, errors.New(problem.Message)
		}
		if version, err := registry.CurrentVersion(ctx, a.Run, a.Root, pkg, a.Cfg.Git.TagPrefix); err == nil {
			pkg.Version = version
		}
		tag, err := release.TagFor(a.Cfg, scope, pkg, pkg.Version)
		if err != nil {
			tag = a.Cfg.Git.TagPrefix + pkg.Version
		}
		entry := StatusPackage{Name: name, Version: pkg.Version, Tag: tag, Published: "unknown"}
		if exists, err := a.Git.TagExists(ctx, tag); err == nil {
			entry.TagExists = exists
		}
		if !offline {
			if status, err := registry.Precheck(ctx, a.HTTP, pkg, pkg.Version); err == nil {
				switch status {
				case ecosystem.Exists:
					entry.Published = "yes"
				case ecosystem.Absent:
					entry.Published = "no"
				}
			}
		}
		report.Packages = append(report.Packages, entry)
	}
	if len(report.Packages) > 0 {
		// The top-level fields keep their pre-multi-package shape for scripts.
		first := report.Packages[0]
		report.Tag = first.Tag
		report.TagExists = first.TagExists
		report.Published = first.Published
	}

	records, err := changes.LoadDir(filepath.Join(a.Root, a.Cfg.Changes.Dir))
	if err == nil {
		for _, record := range records {
			report.Pending = append(report.Pending, fsx.Rel(a.Root, record.Path))
		}
	}
	last, err := state.LatestRun(a.Root)
	if err == nil {
		report.LastRun = last
	}
	return report, nil
}

// findPackage resolves a registered package name against the detected packages.
func (a *App) findPackage(detected []ecosystem.Package, name string) (ecosystem.Package, bool) {
	for _, pkg := range detected {
		if pkg.IdentityName() == name || pkg.Name == name {
			return pkg, true
		}
	}
	pkgCfg, ok := a.Cfg.Packages[name]
	if ok {
		for _, pkg := range detected {
			if pkg.Path == pkgCfg.Path {
				pkg.Identity = name
				return pkg, true
			}
		}
	}
	if len(detected) == 1 {
		return detected[0], true
	}
	return ecosystem.Package{}, false
}
