package app

import (
	"context"
	"encoding/json"
	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"os"
	"path/filepath"
	"strings"
)

// CheckReport is the machine readable result of qegg check.
type CheckReport struct {
	SchemaVersion int               `json:"schema_version"`
	OK            bool              `json:"ok"`
	Ecosystem     string            `json:"ecosystem,omitempty"`
	Package       string            `json:"package,omitempty"`
	Version       string            `json:"version,omitempty"`
	Manifest      string            `json:"manifest,omitempty"`
	Mode          string            `json:"mode"`
	Tools         map[string]string `json:"tools"`
	Verify        VerifyStatus      `json:"verify"`
	Packages      PackageStatus     `json:"packages"`
	Entries       []PackageEntry    `json:"entries,omitempty"`
	Problems      []Problem         `json:"problems"`
}

// VerifyStatus reports the configured verification commands.
type VerifyStatus struct {
	Commands []string `json:"commands"`
	Timeout  string   `json:"timeout"`
	Problems []string `json:"problems,omitempty"`
}

// PackageStatus reports registered and detected packages.
type PackageStatus struct {
	Registered   []string `json:"registered"`
	Detected     []string `json:"detected"`
	Unregistered []string `json:"unregistered"`
}

// PackageEntry is one checked package.
type PackageEntry struct {
	Name     string `json:"name"`
	Resolver string `json:"resolver"`
	Version  string `json:"version"`
	Manifest string `json:"manifest,omitempty"`
}

// Problem is one actionable check failure.
type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// Check validates configuration, ecosystem, tools and change records.
func (a *App) Check(ctx context.Context, ci bool) (*CheckReport, error) {
	report := &CheckReport{
		SchemaVersion: 1,
		Mode:          a.Cfg.Release.Mode,
		Tools:         map[string]string{},
		Verify:        VerifyStatus{Commands: a.Cfg.Verify.Commands, Timeout: a.Cfg.Verify.Timeout},
	}

	detected, err := registry.DetectAll(a.Root)
	if err != nil {
		report.Problems = append(report.Problems, Problem{Code: "detect-failed", Message: err.Error()})
		return report, nil
	}
	report.Packages.Registered = a.Cfg.PackageNames()
	for _, pkg := range detected {
		report.Packages.Detected = append(report.Packages.Detected, pkg.Name)
		if _, ok := a.Cfg.Packages[pkg.Name]; !ok {
			report.Packages.Unregistered = append(report.Packages.Unregistered, pkg.Name)
		}
	}
	if len(report.Packages.Unregistered) > 0 {
		report.Problems = append(report.Problems, Problem{
			Code:    "unregistered-package",
			Message: i18n.Tf("check.problem.unregistered_package", strings.Join(report.Packages.Unregistered, ", ")),
			Fix:     i18n.T("check.fix.run_init"),
		})
	}

	names := a.Cfg.PackageNames()
	if len(names) == 0 {
		for _, pkg := range detected {
			names = append(names, pkg.Name)
		}
	}
	for _, name := range names {
		pkg, ok := a.Cfg.Packages[name]
		if !ok {
			continue
		}
		if _, known := registry.ByID(pkg.Resolver); !known {
			report.Problems = append(report.Problems, Problem{
				Code:    "unknown-resolver",
				Message: i18n.Tf("check.problem.unknown_resolver", name, pkg.Resolver),
				Fix:     i18n.T("check.fix.unknown_resolver"),
			})
		}
	}
	if unknown := registry.UnknownRegistries(a.Cfg.Publish.Registries); len(unknown) > 0 {
		report.Problems = append(report.Problems, Problem{
			Code:    "unknown-registry",
			Message: i18n.Tf("check.problem.unknown_registry", strings.Join(unknown, ", "), strings.Join(registry.PublishTargets(), ", ")),
			Fix:     i18n.T("check.fix.unknown_registry"),
		})
	}
	seenTools := map[string]bool{}
	for _, name := range names {
		pkg, ok := a.findPackage(detected, name)
		if !ok {
			report.Problems = append(report.Problems, Problem{
				Code:    "package-missing",
				Message: i18n.Tf("check.problem.package_missing", name),
				Fix:     i18n.T("check.fix.run_init"),
			})
			continue
		}
		version, err := registry.CurrentVersion(ctx, a.Run, a.Root, pkg, a.Cfg.Git.TagPrefix)
		if err != nil {
			report.Problems = append(report.Problems, Problem{Code: "version-failed", Message: err.Error()})
			continue
		}
		pkg.Version = version
		report.Entries = append(report.Entries, PackageEntry{
			Name: pkg.Name, Resolver: pkg.Resolver, Version: pkg.Version, Manifest: pkg.Manifest,
		})
		for _, tool := range requiredTools(pkg.Resolver) {
			if seenTools[tool.command] {
				continue
			}
			seenTools[tool.command] = true
			if execx.LookPath(tool.command) {
				report.Tools[tool.name] = tool.hint
			} else {
				report.Tools[tool.name] = ""
				report.Problems = append(report.Problems, Problem{
					Code:    "tool-missing",
					Message: i18n.Tf("check.problem.tool_missing", tool.command, tool.hint),
					Fix:     tool.install,
				})
			}
		}
	}
	if len(report.Entries) == 0 {
		report.Problems = append(report.Problems, Problem{
			Code:    "no-package",
			Message: i18n.T("check.problem.no_package"),
		})
		return report, nil
	}
	report.Ecosystem = report.Entries[0].Resolver
	report.Package = report.Entries[0].Name
	report.Version = report.Entries[0].Version
	report.Manifest = report.Entries[0].Manifest
	if a.Cfg.Release.VersionScope == config.VersionScopeShared && len(report.Entries) > 1 {
		base := report.Entries[0]
		for _, entry := range report.Entries[1:] {
			if entry.Version == base.Version {
				continue
			}
			report.Problems = append(report.Problems, Problem{
				Code:    "shared-version-mismatch",
				Message: i18n.Tf("check.problem.shared_version_mismatch", entry.Name, entry.Version, base.Name, base.Version),
				Fix:     i18n.T("check.fix.shared_version"),
			})
		}
		for _, entry := range report.Entries {
			if !registry.IsTagVersioned(entry.Resolver) {
				continue
			}
			report.Problems = append(report.Problems, Problem{
				Code:    "shared-tag-versioned",
				Message: i18n.Tf("check.problem.shared_tag_versioned", entry.Name),
				Fix:     i18n.T("check.fix.shared_tag_versioned"),
			})
			break
		}
	}

	if len(a.Cfg.Verify.Commands) == 0 {
		report.Verify.Problems = append(report.Verify.Problems, i18n.T("check.problem.verify_empty"))
	}
	if a.Cfg.Release.Mode == config.ModeCommit {
		report.Verify.Problems = append(report.Verify.Problems, i18n.T("check.problem.commit_mode"))
	}

	records, err := changes.LoadDir(filepath.Join(a.Root, a.Cfg.Changes.Dir))
	if err != nil {
		report.Problems = append(report.Problems, Problem{Code: "record-invalid", Message: err.Error()})
	} else {
		for _, record := range records {
			if err := record.Validate(report.Packages.Registered); err != nil {
				report.Problems = append(report.Problems, Problem{
					Code:    "record-invalid",
					Message: i18n.Tf("check.problem.record_invalid", record.Path, err),
					Fix:     i18n.T("check.fix.record_invalid"),
				})
			}
		}
	}

	if ci {
		if problem := a.ciRecordProblem(ctx); problem != nil {
			report.Problems = append(report.Problems, *problem)
		}
	}

	report.OK = len(report.Problems) == 0
	return report, nil
}

func (a *App) ciRecordProblem(ctx context.Context) *Problem {
	base, head := ciDiffRefs()
	if base == "" || head == "" {
		return nil
	}
	files, err := a.Git.ChangedNamesBetween(ctx, base, head)
	if err != nil {
		return nil
	}
	scope := a.Cfg.Changes.Scope
	changesPrefix := filepath.ToSlash(filepath.Clean(a.Cfg.Changes.Dir))
	sourceTouched := false
	recordAdded := false
	for _, file := range files {
		clean := filepath.ToSlash(file)
		if strings.HasPrefix(clean, changesPrefix+"/") && strings.HasSuffix(clean, ".md") && clean != changesPrefix+"/README.md" {
			recordAdded = true
			continue
		}
		if changes.MatchesScope(clean, scope) {
			sourceTouched = true
		}
	}
	if sourceTouched && !recordAdded {
		return &Problem{
			Code:    "missing-record",
			Message: i18n.T("check.problem.missing_record"),
			Fix:     i18n.T("check.fix.missing_record"),
		}
	}
	return nil
}

type toolRequirement struct {
	name    string
	command string
	hint    string
	install string
}

func requiredTools(resolver string) []toolRequirement {
	spec, ok := registry.SpecFor(resolver)
	if !ok {
		return nil
	}
	out := make([]toolRequirement, 0, len(spec.Tools))
	for _, tool := range spec.Tools {
		out = append(out, toolRequirement{
			name:    tool.Name,
			command: tool.Command,
			hint:    i18n.T("tool." + tool.Name + ".purpose"),
			install: i18n.T("tool." + tool.Name + ".install"),
		})
	}
	return out
}

// ciDiffRefs reads the pull request base and head SHAs from the CI event.
func ciDiffRefs() (string, string) {
	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var event struct {
		PullRequest *struct {
			Base struct {
				SHA string `json:"sha"`
			} `json:"base"`
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &event); err != nil || event.PullRequest == nil {
		return "", ""
	}
	return event.PullRequest.Base.SHA, event.PullRequest.Head.SHA
}
