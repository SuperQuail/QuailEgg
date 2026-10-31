package release

import (
	"errors"
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
	"github.com/SuperQuail/QuailEgg/internal/release/assets"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"net/http"
	"time"
)

// Phases of a release run.
const (
	PhaseNone    = "none"
	PhaseVersion = "version"
	PhasePublish = "publish"
)

// ErrMultiPackage is returned when a repository registers more than one package.
var ErrMultiPackage = errors.New("multi-package releases are not supported in v0.1")

// Options controls execution.
type Options struct {
	AllowDirty      bool
	SkipVerify      bool
	NoPush          bool
	NoGitHubRelease bool
	NoPublish       bool
	NoAssets        bool
	// AssetsDir is a directory of prebuilt files to attach instead of building
	// the assets locally. The managed workflow downloads the build jobs'
	// artifacts there.
	AssetsDir string
}

// Deps bundles everything the executor needs.
type Deps struct {
	Root    string
	Cfg     *config.Config
	Git     GitOps
	Run     execx.Runner
	UI      *ui.Printer
	Now     func() time.Time
	HTTP    *http.Client
	Options Options

	// QeggVersion, QeggCommit and QeggDevelop identify the running qegg build
	// in the marker and the local run record.
	QeggVersion string
	QeggCommit  string
	QeggDevelop bool
}

// toolInfo returns the qegg identifier stored in release state, or nil when the
// caller did not provide it (tests that build Deps by hand).
func (d *Deps) toolInfo() *state.ToolInfo {
	if d.QeggVersion == "" && d.QeggCommit == "" {
		return nil
	}
	return &state.ToolInfo{Version: d.QeggVersion, Commit: d.QeggCommit, Develop: d.QeggDevelop}
}

// ProjectInfo describes the release target.
type ProjectInfo struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
	Manifest  string `json:"manifest"`
	Root      string `json:"root"`
}

// VersionInfo describes the version transition.
type VersionInfo struct {
	Current string `json:"current"`
	Next    string `json:"next"`
	Bump    string `json:"bump"`
}

// PackagePlan is one package release.
type PackagePlan struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Resolver string `json:"resolver"`
	Publish  bool   `json:"publish"`
	Current  string `json:"current"`
	Next     string `json:"next"`
	Bump     string `json:"bump"`
	Tag      string `json:"tag,omitempty"`
	Manifest string `json:"-"`
}

// ReleasePlan is one release unit: a package set that shares one version and
// one git tag. A single-package or shared-version repository has exactly one
// unit; a package-scoped multi-package repository has one unit per releasing
// package.
type ReleasePlan struct {
	Name       string   `json:"name,omitempty"`
	Version    string   `json:"version"`
	Tag        string   `json:"tag"`
	PrevTag    string   `json:"previous_tag,omitempty"`
	Packages   []string `json:"packages"`
	Prerelease string   `json:"prerelease,omitempty"`
}

// ChangePlan is one consumed record.
type ChangePlan struct {
	File       string   `json:"file"`
	Type       string   `json:"type"`
	Scope      string   `json:"scope,omitempty"`
	Bump       string   `json:"bump"`
	Packages   []string `json:"packages,omitempty"`
	Body       string   `json:"body"`
	Commit     string   `json:"commit,omitempty"`
	CommitURL  string   `json:"commit_url,omitempty"`
	PullNumber int      `json:"pull_number,omitempty"`
	PullURL    string   `json:"pull_url,omitempty"`
	Author     string   `json:"author,omitempty"`
}

// VerifyInfo describes the pre-release commands.
type VerifyInfo struct {
	Commands []string `json:"commands"`
	Timeout  string   `json:"timeout"`
	Source   string   `json:"source"`
}

// StepInfo is one planned step.
type StepInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Plan is the complete release intent.
type Plan struct {
	SchemaVersion int           `json:"schema_version"`
	Phase         string        `json:"phase"`
	HasChanges    bool          `json:"has_changes"`
	Channel       string        `json:"channel,omitempty"`
	Graduating    bool          `json:"graduating,omitempty"`
	Fingerprint   string        `json:"fingerprint"`
	Project       ProjectInfo   `json:"project"`
	VersionScope  string        `json:"version_scope,omitempty"`
	Version       VersionInfo   `json:"version"`
	Packages      []PackagePlan `json:"packages"`
	Releases      []ReleasePlan `json:"releases,omitempty"`
	Changes       []ChangePlan  `json:"changes"`
	Verify        VerifyInfo    `json:"verify"`
	Steps         []StepInfo    `json:"steps"`
	Warnings      []string      `json:"warnings"`
	Tag           string        `json:"tag"`
	PreviousTag   string        `json:"previous_tag,omitempty"`
	Assets        *assets.Plan  `json:"assets,omitempty"`
	Marker        *state.Marker `json:"-"`
	Repo          github.Repo   `json:"-"`
}
