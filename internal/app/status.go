package app

import (
	"context"
	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/semver"
	"path/filepath"
)

// StatusReport is the machine readable output of qegg status.
type StatusReport struct {
	SchemaVersion int              `json:"schema_version"`
	Marker        *state.Marker    `json:"marker,omitempty"`
	Tag           string           `json:"tag"`
	TagExists     bool             `json:"tag_exists"`
	Published     string           `json:"published"`
	Pending       []string         `json:"pending_changes"`
	LastRun       *state.RunRecord `json:"last_run,omitempty"`
}

// Status reports the current release state.
func (a *App) Status(ctx context.Context, offline bool) (*StatusReport, error) {
	report := &StatusReport{SchemaVersion: 1, Published: "unknown"}
	rel, err := state.LoadMarker(a.Root)
	if err != nil {
		return nil, err
	}
	report.Marker = rel

	detected, err := registry.DetectAll(a.Root)
	if err != nil {
		return nil, err
	}
	if primary, ok := a.primaryPackage(detected); ok {
		if version, err := registry.CurrentVersion(ctx, a.Run, a.Root, primary, a.Cfg.Git.TagPrefix); err == nil {
			primary.Version = version
		}
		tag := semver.Tag(a.Cfg.Git.TagPrefix, primary.Version)
		report.Tag = tag
		exists, err := a.Git.TagExists(ctx, tag)
		if err == nil {
			report.TagExists = exists
		}
		if !offline {
			if status, err := registry.Precheck(ctx, a.HTTP, primary, primary.Version); err == nil {
				switch status {
				case ecosystem.Exists:
					report.Published = "yes"
				case ecosystem.Absent:
					report.Published = "no"
				}
			}
		}
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
