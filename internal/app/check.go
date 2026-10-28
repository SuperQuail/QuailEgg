package app

import (
	"context"
	"encoding/json"
	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
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

	primary, ok := a.primaryPackage(detected)
	if !ok {
		report.Problems = append(report.Problems, Problem{
			Code:    "no-package",
			Message: i18n.T("check.problem.no_package"),
		})
		return report, nil
	}
	version, versionErr := registry.CurrentVersion(ctx, a.Run, a.Root, primary, a.Cfg.Git.TagPrefix)
	if versionErr == nil {
		primary.Version = version
	}
	report.Ecosystem = primary.Resolver
	report.Package = primary.Name
	report.Version = primary.Version
	report.Manifest = primary.Manifest
	if versionErr != nil {
		report.Problems = append(report.Problems, Problem{Code: "version-failed", Message: versionErr.Error()})
	}

	for _, tool := range requiredTools(primary.Resolver) {
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

func (a *App) primaryPackage(detected []ecosystem.Package) (ecosystem.Package, bool) {
	if len(a.Cfg.Packages) == 1 {
		for name, pkg := range a.Cfg.Packages {
			for _, candidate := range detected {
				if candidate.Name == name || candidate.Path == pkg.Path {
					return candidate, true
				}
			}
			if len(detected) == 1 {
				return detected[0], true
			}
			return ecosystem.Package{}, false
		}
	}
	if len(detected) == 1 {
		return detected[0], true
	}
	return ecosystem.Package{}, false
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
