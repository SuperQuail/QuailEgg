package release

import (
	"errors"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
)

// plannedVersions maps every package the round releases to the version it
// writes. A package that is not part of a release unit is missing from the map
// and keeps the version its manifest already has.
func (e *executor) plannedVersions() map[string]string {
	next := map[string]string{}
	for _, release := range e.plan.Releases {
		for _, name := range release.Packages {
			next[name] = release.Version
		}
	}
	return next
}

// plannedTargets lists the inspected packages in plan order, each carrying the
// version this round writes.
func (e *executor) plannedTargets(inspected []ecosystem.Package) []ecosystem.Package {
	next := e.plannedVersions()
	targets := make([]ecosystem.Package, 0, len(e.plan.Packages))
	for _, planned := range e.plan.Packages {
		target, ok := findInspected(inspected, planned.Name)
		if !ok {
			continue
		}
		if version, ok := next[planned.Name]; ok {
			target.Version = version
		}
		targets = append(targets, target)
	}
	return targets
}

// plannedGroups groups the planned targets by ecosystem.
func (e *executor) plannedGroups(inspected []ecosystem.Package) map[string][]ecosystem.Package {
	groups := map[string][]ecosystem.Package{}
	for _, target := range e.plannedTargets(inspected) {
		groups[target.Resolver] = append(groups[target.Resolver], target)
	}
	return groups
}

// preflightManifests checks every manifest location the version phase is about
// to rewrite before the first file is written. Writing package by package makes
// a manifest the round cannot update safely show up only after the earlier
// packages were rewritten; this check runs on the whole batch first, so the
// release stops with every manifest byte-identical.
func (e *executor) preflightManifests(inspected []ecosystem.Package) error {
	round := ecosystem.VersionRound{Packages: e.plannedTargets(inspected)}
	for _, release := range e.plan.Releases {
		for _, name := range release.Packages {
			target, ok := findInspected(inspected, name)
			if !ok {
				continue
			}
			target.Version = release.Version
			round.Own = append(round.Own, target)
		}
	}
	findings, err := registry.PreflightVersions(e.d.Root, round)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		return nil
	}
	return errors.New(registry.PreflightMessage(findings))
}
