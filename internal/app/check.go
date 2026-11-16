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
	"github.com/SuperQuail/QuailEgg/internal/release"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"os"
	"path/filepath"
	"strings"
)

// CheckReport is the machine readable result of qegg check.
type CheckReport struct {
	SchemaVersion  int               `json:"schema_version"`
	OK             bool              `json:"ok"`
	Ecosystem      string            `json:"ecosystem,omitempty"`
	Package        string            `json:"package,omitempty"`
	Version        string            `json:"version,omitempty"`
	PendingVersion string            `json:"pending_version,omitempty"`
	Manifest       string            `json:"manifest,omitempty"`
	Mode           string            `json:"mode"`
	Tools          map[string]string `json:"tools"`
	Verify         VerifyStatus      `json:"verify"`
	Packages       PackageStatus     `json:"packages"`
	Entries        []PackageEntry    `json:"entries,omitempty"`
	Problems       []Problem         `json:"problems"`
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
	registeredManifest := make(map[string]bool, len(report.Packages.Registered))
	for _, key := range report.Packages.Registered {
		registeredManifest[a.Cfg.ManifestName(key)] = true
	}
	for _, pkg := range detected {
		report.Packages.Detected = append(report.Packages.Detected, pkg.Name)
		if !registeredManifest[pkg.Name] {
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
	if unknown := registry.UnknownRegistries(a.Cfg.Publish.Registries); len(unknown) > 0 {
		report.Problems = append(report.Problems, Problem{
			Code:    "unknown-registry",
			Message: i18n.Tf("check.problem.unknown_registry", strings.Join(unknown, ", "), strings.Join(registry.PublishTargets(), ", ")),
			Fix:     i18n.T("check.fix.unknown_registry"),
		})
	}
	seenTools := map[string]bool{}
	for _, name := range names {
		pkg, problem := a.inspectPackage(detected, name)
		if problem != nil {
			report.Problems = append(report.Problems, *problem)
			continue
		}
		version, err := registry.CurrentVersion(ctx, a.Run, a.Root, pkg, a.Cfg.Git.TagPrefix)
		if err != nil {
			report.Problems = append(report.Problems, Problem{Code: "version-failed", Message: err.Error()})
			continue
		}
		pkg.Version = version
		if strings.Contains(version, "${") {
			report.Problems = append(report.Problems, Problem{
				Code:    "version-unresolved",
				Message: i18n.Tf("check.problem.version_unresolved", pkg.IdentityName(), version),
				Fix:     i18n.T("check.fix.version_unresolved"),
			})
		}
		report.Entries = append(report.Entries, PackageEntry{
			Name: pkg.IdentityName(), Resolver: pkg.Resolver, Version: pkg.Version, Manifest: pkg.Manifest,
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
	// A release can be in flight: the version phase writes the version files and
	// bumps the manifests, while a tag-versioned package keeps its previous tag
	// until the publish phase creates the new one. The marker names the version
	// being prepared, so the shared-version rules and the version files are
	// checked against it rather than against manifests that legitimately
	// disagree in between.
	versions := packageVersions(report.Entries)
	pending, pendingProblems := a.pendingReleaseVersion(ctx, versions)
	report.Problems = append(report.Problems, pendingProblems...)
	report.PendingVersion = pending.Version
	// A marker that cannot be read leaves the pending version unknown, and an
	// empty version reads as "no release in flight": the shared rules would then
	// hold the packages to each other's checked out versions and blame one of
	// them for drift the marker explains, with a fix that is wrong while the
	// release is still running. Keep the marker problem alone instead.
	if a.Cfg.Release.VersionScope == config.VersionScopeShared && len(report.Entries) > 1 && len(pendingProblems) == 0 {
		// A release that leaves a package out never writes its version, so the
		// untouched package would be blamed for drift it did not cause. Report
		// the missing coverage instead.
		if pending.Version != "" && len(pending.Uncovered) > 0 {
			report.Problems = append(report.Problems, Problem{
				Code:    "shared-version-uncovered",
				Message: i18n.Tf("check.problem.shared_version_uncovered", pending.Version, strings.Join(pending.Uncovered, ", ")),
				Fix:     i18n.T("check.fix.shared_version_uncovered"),
			})
		} else {
			report.Problems = append(report.Problems, a.sharedVersionProblems(versions, pending.Version)...)
		}
	}
	// A package-scoped release tags every package separately, so a package
	// without a manifest version cannot be planned at all. The plan refuses such
	// a repository, so check reports it before a release gets there.
	if a.Cfg.Release.VersionScope != config.VersionScopeShared && len(report.Entries) > 1 {
		for _, entry := range report.Entries {
			if !registry.IsTagVersioned(entry.Resolver) {
				continue
			}
			report.Problems = append(report.Problems, Problem{
				Code:    "package-tag-versioned",
				Message: i18n.Tf("check.problem.package_tag_versioned", entry.Name),
				Fix:     i18n.T("check.fix.package_tag_versioned"),
			})
			break
		}
	}

	if len(a.Cfg.Version.File) > 0 {
		report.Problems = append(report.Problems, a.versionFileProblems(report.Entries, pending.Version, pendingProblems)...)
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

// pendingReleaseVersion returns the release the marker is preparing, empty when
// no release is in flight. The version files and the shared-version rules are
// checked against it because a tag-versioned package only reports the new
// version once its tag exists, so the manifests disagree until the publish
// phase runs. A returned problem means the pending version is unknown rather
// than absent, which the caller has to tell apart before it compares anything.
//
// A missing repository is the one exception: without git the tags cannot be read
// at all, so the version is reported as absent and no problem. app.New always
// sets Git, so only a hand-built App reaches that branch.
func (a *App) pendingReleaseVersion(ctx context.Context, pkgs []release.PackageVersion) (release.PendingRelease, []Problem) {
	// A nil *git.Repo stored in the TagChecker interface is not a nil interface,
	// so a missing repository has to be caught here, before the release package
	// dereferences it.
	if a.Git == nil {
		return release.PendingRelease{}, nil
	}
	marker, err := state.LoadMarker(a.Root)
	if err != nil {
		return release.PendingRelease{}, []Problem{{
			Code:    "marker-invalid",
			Message: i18n.Tf("check.problem.marker_invalid", err),
			Fix:     i18n.T("check.fix.marker_invalid"),
		}}
	}
	pending, err := release.PendingSharedVersion(ctx, a.Cfg, a.Git, marker, pkgs)
	if err != nil {
		return release.PendingRelease{}, []Problem{{
			Code:    "pending-unresolved",
			Message: i18n.Tf("check.problem.pending_unresolved", err),
			Fix:     i18n.T("check.fix.pending_unresolved"),
		}}
	}
	return pending, nil
}

// packageVersions is the release view of the checked entries.
func packageVersions(entries []PackageEntry) []release.PackageVersion {
	pkgs := make([]release.PackageVersion, 0, len(entries))
	for _, entry := range entries {
		pkgs = append(pkgs, release.PackageVersion{
			Name: entry.Name, Version: entry.Version, Resolver: entry.Resolver,
		})
	}
	return pkgs
}

// sharedVersionProblems validates the packages of a shared-version repository
// against the version a pending release is preparing.
func (a *App) sharedVersionProblems(pkgs []release.PackageVersion, pending string) []Problem {
	problems := make([]Problem, 0, len(pkgs))
	for _, violation := range release.CheckSharedVersions(pkgs, pending) {
		switch violation.Code {
		case release.SharedVersionMismatch:
			problems = append(problems, Problem{
				Code:    "shared-version-mismatch",
				Message: i18n.Tf("check.problem.shared_version_mismatch", violation.Package, violation.Version, violation.Other, violation.OtherVersion),
				Fix:     i18n.T("check.fix.shared_version"),
			})
		case release.SharedVersionPending:
			// The release is already running, so "align the versions by hand" is
			// the wrong advice: the version phase wrote the pending version and a
			// tag-versioned package follows once its tag exists. Point at the
			// pending release instead. A shallow clone hides the finished tags and
			// makes a completed release look pending, so the fix offers fetching
			// the tags before it suggests deleting the marker.
			problems = append(problems, Problem{
				Code:    "shared-version-pending",
				Message: i18n.Tf("check.problem.shared_version_pending", violation.Package, violation.Version, violation.OtherVersion),
				Fix:     i18n.T("check.fix.shared_version_pending"),
			})
		case release.SharedTagSpelling:
			problems = append(problems, Problem{
				Code:    "shared-tag-spelling",
				Message: i18n.Tf("check.problem.shared_tag_spelling", violation.Package, violation.PackageTag, violation.Other, violation.ReferenceTag),
				Fix:     i18n.T("check.fix.shared_tag_spelling"),
			})
		default:
			// A new violation code has to be registered above, or check reports it
			// with the message and fix of a version mismatch.
			problems = append(problems, Problem{
				Code:    "shared-version-mismatch",
				Message: i18n.Tf("check.problem.shared_version_mismatch", violation.Package, violation.Version, violation.Other, violation.OtherVersion),
				Fix:     i18n.T("check.fix.shared_version"),
			})
		}
	}
	return problems
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

// versionFileProblems verifies the configured version files against the one
// release version of the repository. A pending release supplies that version:
// a tag-versioned package only reaches it once its tag exists, so the checked
// out versions cannot be compared against each other mid-release. A pending
// problem means that version is unknown rather than absent, and the files cannot
// be verified at all.
func (a *App) versionFileProblems(entries []PackageEntry, expected string, pendingProblems []Problem) []Problem {
	if len(pendingProblems) > 0 {
		// Falling back to "every package must agree" would blame a shared
		// repository for versions that legitimately differ while a release is in
		// flight, and the fix would suggest the scope it already uses. Report the
		// unverified state instead; the marker problem carries the fix.
		return []Problem{{
			Code:    "version-file-unverified",
			Message: i18n.Tf("check.problem.version_file_unverified", pendingProblems[0].Message),
		}}
	}
	// The caller passes the pending release version when one is in flight. The
	// packages may legitimately sit elsewhere mid-release, so the drift message
	// has to name the pending release instead of "the package version", which
	// would quote a number the packages are not supposed to carry yet.
	fromPending := expected != ""
	if expected == "" {
		versions := make(map[string]bool, len(entries))
		for _, entry := range entries {
			versions[entry.Version] = true
		}
		if len(versions) != 1 {
			return []Problem{{
				Code:    "version-file-ambiguous",
				Message: i18n.T("check.problem.version_file_ambiguous"),
				Fix:     i18n.T("check.fix.version_file_ambiguous"),
			}}
		}
		expected = entries[0].Version
	}
	problems := make([]Problem, 0, len(a.Cfg.Version.File))
	for _, file := range a.Cfg.Version.File {
		current, err := file.Read(a.Root)
		if err != nil {
			problems = append(problems, Problem{
				Code:    "version-file-invalid",
				Message: err.Error(),
				Fix:     i18n.T("check.fix.version_file_invalid"),
			})
			continue
		}
		if current == expected {
			continue
		}
		message := i18n.Tf("check.problem.version_file_drift", file.Path, current, expected)
		if fromPending {
			message = i18n.Tf("check.problem.version_file_drift_pending", file.Path, current, expected)
		}
		problems = append(problems, Problem{
			Code:    "version-file-drift",
			Message: message,
			Fix:     i18n.T("check.fix.version_file_drift"),
		})
	}
	return problems
}

// inspectPackage reads a configured package from its path. When the
// configuration does not list the name it falls back to the detected package,
// which keeps single-package repositories working without a config entry.
func (a *App) inspectPackage(detected []ecosystem.Package, name string) (ecosystem.Package, *Problem) {
	if cfg, ok := a.Cfg.Packages[name]; ok {
		if _, known := registry.ByID(cfg.Resolver); !known {
			return ecosystem.Package{}, &Problem{
				Code:    "unknown-resolver",
				Message: i18n.Tf("check.problem.unknown_resolver", name, cfg.Resolver),
				Fix:     i18n.T("check.fix.unknown_resolver"),
			}
		}
		pkg, err := registry.Inspect(a.Root, ecosystem.Package{
			Name:     a.Cfg.ManifestName(name),
			Identity: name,
			Path:     cfg.Path,
			Resolver: cfg.Resolver,
		})
		if err != nil {
			return ecosystem.Package{}, &Problem{
				Code:    "inspect-failed",
				Message: err.Error(),
				Fix:     i18n.T("check.fix.inspect_failed"),
			}
		}
		return pkg, nil
	}
	pkg, ok := a.findPackage(detected, name)
	if !ok {
		return ecosystem.Package{}, &Problem{
			Code:    "package-missing",
			Message: i18n.Tf("check.problem.package_missing", name),
			Fix:     i18n.T("check.fix.run_init"),
		}
	}
	return pkg, nil
}
