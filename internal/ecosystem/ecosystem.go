// Package ecosystem defines the package model and the interfaces every
// supported language adapter implements.
package ecosystem

import (
	"context"
	"errors"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"net/http"
)

// Package is one discovered package.
type Package struct {
	Name     string
	Path     string // slash separated, relative to the repository root
	Resolver string // npm | python | rust | go
	Version  string // manifest version; empty when the ecosystem derives it from tags
	// Inherited reports that Version comes from a workspace manifest rather than
	// from this package's own manifest (Cargo's version.workspace = true).
	Inherited bool
	Publish   bool
	Manifest  string // slash separated, relative to the repository root
	Deps      []string
	// Identity is the configuration key qegg uses for the package. It is empty
	// for packages returned by detection; configured packages carry it so the
	// release plan can tell two manifests with the same name apart.
	Identity string
}

// IdentityName returns the configured key, or the manifest name for detected
// packages that have no configuration entry.
func (p Package) IdentityName() string {
	if p.Identity != "" {
		return p.Identity
	}
	return p.Name
}

// Tool describes an external command required by an ecosystem. The purpose
// and install hint live in the message catalog under tool.<name>.*.
type Tool struct {
	Name    string
	Command string
}

// Spec is the declarative description of one supported ecosystem. Adding a
// language means adding a subpackage that returns a Spec; user-facing text,
// workflow setup and tool checks are derived from it instead of being
// hardcoded in messages.
type Spec struct {
	ID        string
	Language  string
	Manifests []string
	// PublishTargets lists the registry names this ecosystem publishes to
	// (npm, pypi, crates, go). qegg init derives the publish configuration
	// and the generated workflow credentials from it.
	PublishTargets []string
	// TagPrefix is the prefix a tag-derived version needs. An empty value
	// means the ecosystem accepts any configured prefix.
	TagPrefix string
	// WorkflowSetup is a YAML sequence starting at column zero. qegg init
	// indents it into the release workflow's steps list.
	WorkflowSetup string
	// WorkflowEnv is a YAML mapping starting at column zero. qegg init
	// indents it into the release step's environment block.
	WorkflowEnv string
	Tools       []Tool
}

// Status is a registry existence answer.
type Status int

// Registry existence answers.
const (
	Unknown Status = iota
	Exists
	Absent
)

// ErrUnsupported is returned for ecosystems without a publisher.
var ErrUnsupported = errors.New("publishing is not supported for this ecosystem")

// Publisher uploads one package to its registry.
type Publisher interface {
	Name() string
	Precheck(ctx context.Context, client *http.Client, pkg Package, releaseVersion string) (Status, error)
	Build(ctx context.Context, run execx.Runner, root string, pkg Package) error
	Publish(ctx context.Context, run execx.Runner, root string, pkg Package, releaseVersion string) error
}

// Ecosystem adapts one package manager: detection, manifest version writes and
// publishing all live in the same adapter.
type Ecosystem interface {
	Spec() Spec
	Detect(root string) ([]Package, error)
	SetVersion(root string, pkg Package, next string) error
	Publisher() Publisher
}

// VersionStyle is implemented by ecosystems whose version spelling differs from
// the tool's SemVer form, or whose git tag spelling differs from the manifest.
// Python keeps SemVer pre-release spelling in pyproject.toml ("2.0.2-a14") but
// tags and PyPI use the PEP 440 form ("2.0.2a14").
type VersionStyle interface {
	// Prerelease renders one prerelease version in the ecosystem spelling.
	Prerelease(base, channel string, counter int) (string, error)
	// SplitPrerelease parses a version in the ecosystem spelling. It reports ok
	// only for a prerelease version of a known channel.
	SplitPrerelease(version string) (base, channel string, counter int, ok bool)
	// TagVersion converts a manifest version into the version embedded in a tag.
	TagVersion(version string) string
}

// TagVersioner is implemented by ecosystems whose current version is the latest
// git release tag instead of a manifest field (Go modules). The release planner
// resolves the current version through it and validates the planned next
// version against the module coordinate.
type TagVersioner interface {
	// CurrentVersion returns the latest released version reachable from HEAD,
	// or 0.0.0 when the repository has no release tag yet.
	CurrentVersion(ctx context.Context, run execx.Runner, root string, pkg Package, tagPrefix string) (string, error)
	// CheckVersion rejects a next version that the ecosystem cannot express.
	CheckVersion(pkg Package, next string) error
}

// InitAdvisor is implemented by ecosystems that can suggest configuration
// defaults for qegg init.
type InitAdvisor interface {
	// DefaultVerifyCommands returns the verification commands qegg init writes
	// when the configuration does not set [verify].commands yet.
	DefaultVerifyCommands(root string) []string
}

// Inspector is implemented by ecosystems that can read one package from a
// configured directory without scanning the repository. qegg init still uses
// Detect for suggestions; the release planner reads configured paths through
// Inspect.
type Inspector interface {
	// Inspect reads the package at root/pkg.Path and returns it with Name,
	// Version, Manifest, Publish and Deps filled in.
	Inspect(root string, pkg Package) (Package, error)
}

// VersionSyncer is implemented by ecosystems whose manifests reference each
// other and whose packages may carry different next versions (Cargo workspace
// members, for example). pkgs carries every managed package with Version set
// to the version to write; it returns the repository-relative paths it changed.
type VersionSyncer interface {
	SyncVersions(ctx context.Context, run execx.Runner, root string, pkgs []Package) ([]string, error)
}

// AssetTarget is one os/arch pair of an asset plan an ecosystem builds.
type AssetTarget struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// String renders the target as os/arch.
func (t AssetTarget) String() string { return t.OS + "/" + t.Arch }

// AssetOptions configures asset planning for one package.
type AssetOptions struct {
	Name    string
	Main    string
	Targets []string
	Ldflags []string
}

// AssetPlan describes the assets an ecosystem builds itself.
type AssetPlan struct {
	Name    string
	Main    string
	Targets []AssetTarget
	Ldflags []string
}

// AssetPlanner is implemented by ecosystems whose release assets qegg builds
// itself (Go cross-compiles the main package). Ecosystems without a planner
// attach assets through [release.assets] commands and files instead.
type AssetPlanner interface {
	// PlanAssets resolves the assets of pkg. A nil plan means the package has
	// nothing to attach; warnings are non-fatal.
	PlanAssets(root string, pkg Package, opts AssetOptions) (*AssetPlan, []string, error)
}

// VersionFile describes one file outside the package manifests that may carry
// the package version. Path is relative to the package directory and uses
// forward slashes.
type VersionFile struct {
	Path   string
	Format string
	Key    string
}

// VersionFileAdvisor is implemented by ecosystems that know of files outside
// the package manifest that carry the version, such as Tauri's tauri.conf.json.
// A candidate is a guess: qegg init verifies that the file holds a literal
// version before adding it to the configuration.
type VersionFileAdvisor interface {
	// VersionFileCandidates lists the files that may carry pkg's version.
	VersionFileCandidates(root string, pkg Package) []VersionFile
}

// ManifestScan is the outcome of parsing one manifest found by a bounded
// repository scan. A manifest outside the ecosystem's own workspace detection
// may still describe a package qegg can manage.
type ManifestScan struct {
	Package Package
	// OK reports that the manifest defines a package qegg can manage.
	OK bool
	// Reason names why the manifest cannot be managed yet. It is a stable
	// machine identifier; user-facing text is derived from it.
	Reason string
	// Detail optionally explains the reason in the user's language.
	Detail string
}

// Manifest scanning reasons. The values are stable identifiers used by the
// scan output and must not be translated.
const (
	// ScanReasonNestedGo marks a Go module below the repository root.
	ScanReasonNestedGo = "nested_go_unsupported"
	// ScanReasonNestedWorkspace marks a Cargo member of a nested workspace.
	ScanReasonNestedWorkspace = "nested_workspace_unsupported"
	// ScanReasonWorkspaceVersion marks a member that inherits a missing
	// workspace version.
	ScanReasonWorkspaceVersion = "workspace_version_missing"
)

// ManifestScanner parses one manifest file found by the bounded repository
// scan. pkg carries Path (repository-relative, slash separated), Manifest and
// Resolver; Name and Version may be empty.
type ManifestScanner interface {
	ScanManifest(root string, pkg Package) (ManifestScan, error)
}

// PublishAdvisor is implemented by ecosystems that can tell application
// packages apart from library packages when qegg init suggests whether to
// publish. Returning pkg.Publish unchanged keeps the manifest decision.
type PublishAdvisor interface {
	DefaultPublish(root string, pkg Package) bool
}
