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
	Manifest string `json:"-"`
}

// ChangePlan is one consumed record.
type ChangePlan struct {
	File       string `json:"file"`
	Type       string `json:"type"`
	Scope      string `json:"scope,omitempty"`
	Bump       string `json:"bump"`
	Body       string `json:"body"`
	Commit     string `json:"commit,omitempty"`
	CommitURL  string `json:"commit_url,omitempty"`
	PullNumber int    `json:"pull_number,omitempty"`
	PullURL    string `json:"pull_url,omitempty"`
	Author     string `json:"author,omitempty"`
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
	Version       VersionInfo   `json:"version"`
	Packages      []PackagePlan `json:"packages"`
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
