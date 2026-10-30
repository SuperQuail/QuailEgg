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
	Publish  bool
	Manifest string // slash separated, relative to the repository root
	Deps     []string
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
	ID            string
	Language      string
	Manifests     []string
	Publishable   bool
	BuildRequired bool
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
var ErrUnsupported = errors.New("publishing is not supported for this ecosystem in v0.1")

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
