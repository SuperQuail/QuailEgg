// Package config loads and validates .quailegg.toml.
package config

import (
	"errors"
	"fmt"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/SuperQuail/QuailEgg/internal/versionfile"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SchemaVersion is the only supported configuration schema.
const SchemaVersion = 1

// FileName is the configuration file name.
const FileName = ".quailegg.toml"

// Release modes.
const (
	ModePR     = "pr"
	ModeCommit = "commit"
)

// Version scopes.
const (
	// VersionScopePackage versions every package on its own.
	VersionScopePackage = "package"
	// VersionScopeShared gives every package one version and one tag.
	VersionScopeShared = "shared"
)

// Resolver identifiers.
const (
	ResolverNPM    = "npm"
	ResolverPython = "python"
	ResolverRust   = "rust"
	ResolverGo     = "go"
)

// ErrNotFound is returned when no configuration file exists.
var ErrNotFound = errors.New("no .quailegg.toml found")

// Config mirrors the TOML schema.
type Config struct {
	SchemaVersion int `toml:"schema_version"`
	// Language is a legacy key. The interface language is user-scoped (qegg
	// lang, --lang, QEGG_LANG); the field only keeps existing .quailegg.toml
	// files loadable, because unknown keys are rejected. It has no effect.
	Language string             `toml:"language,omitempty"`
	Project  Project            `toml:"project"`
	Git      Git                `toml:"git"`
	Release  Release            `toml:"release"`
	Changes  Changes            `toml:"changes"`
	Verify   Verify             `toml:"verify"`
	Publish  Publish            `toml:"publish"`
	Packages map[string]Package `toml:"packages"`
	Version  Version            `toml:"version"`
}

// Project holds project level settings.
type Project struct {
	DefaultBranch string `toml:"default_branch"`
	// Repo is the canonical host/owner/name identity of the project repository.
	// It is written by qegg init and lets the hook tell a fork clone apart.
	Repo string `toml:"repo,omitempty"`
}

// Git holds git integration settings.
type Git struct {
	TagPrefix string `toml:"tag_prefix"`
	Remote    string `toml:"remote"`
	// TagFormat renders tags in package scope. Empty uses <slug>-v<version>.
	TagFormat string `toml:"tag_format"`
}

// Release controls how versions are produced and published.
type Release struct {
	Mode          string `toml:"mode"`
	Branch        string `toml:"branch"`
	GitHubRelease bool   `toml:"github_release"`
	CommitMessage string `toml:"commit_message"`
	// VersionScope is "package" (default) or "shared".
	VersionScope string `toml:"version_scope"`
	// Environment is the GitHub Actions environment of the release job.
	Environment string `toml:"environment"`
	Assets      Assets `toml:"assets"`
	// Workflow configures the generated GitHub Actions release workflow.
	Workflow Workflow `toml:"workflow"`
	// Build lists the per-platform builds of the managed workflow.
	Build []BuildJob `toml:"build"`
}

// Workflow modes.
const (
	// WorkflowManaged renders the release workflow from qegg init.
	WorkflowManaged = "managed"
	// WorkflowExternal leaves the release workflow to the repository.
	WorkflowExternal = "external"
)

// DefaultWorkflowRunner is the runner the release job uses.
const DefaultWorkflowRunner = "ubuntu-latest"

// Workflow configures the generated release workflow.
type Workflow struct {
	// Mode is "managed" (qegg renders the workflow) or "external" (the
	// repository keeps its own workflow and qegg generates nothing).
	Mode string `toml:"mode"`
	// Runner is the runner of the release job itself.
	Runner string `toml:"runner"`
	// TimeoutMinutes limits the release job.
	TimeoutMinutes int `toml:"timeout_minutes"`
	// Steps run in the release job before qegg release starts.
	Steps []string `toml:"steps"`
	// YAML replaces Steps with a raw steps block, so custom actions
	// (uses: ...) are possible. The two fields are mutually exclusive.
	YAML string `toml:"yaml"`
	// Env sets environment variables of the shell steps.
	Env map[string]string `toml:"env"`
}

// BuildJob is one platform build of the managed workflow. The generated job
// runs Steps on Runner, uploads Artifacts and hands them to the release job,
// which attaches them to the GitHub Release.
type BuildJob struct {
	Name           string   `toml:"name"`
	Runner         string   `toml:"runner"`
	TimeoutMinutes int      `toml:"timeout_minutes"`
	Steps          []string `toml:"steps"`
	// YAML replaces Steps with a raw steps block and turns off the built-in
	// checkout and ecosystem setup, so the job runs exactly what is written.
	// The artifact upload stays; the two step fields are mutually exclusive.
	YAML string `toml:"yaml"`
	// Env sets environment variables of the shell steps.
	Env       map[string]string `toml:"env"`
	Artifacts []string          `toml:"artifacts"`
}

// Assets configures the files attached to a GitHub release. A Go module is
// built by its ecosystem; commands and files attach the output of a custom
// build instead. An empty section keeps asset handling off.
type Assets struct {
	Enabled *bool    `toml:"enabled"`
	Name    string   `toml:"name"`
	Main    string   `toml:"main"`
	Targets []string `toml:"targets"`
	Ldflags []string `toml:"ldflags"`
	// Commands run in the repository root before the assets are collected.
	Commands []string `toml:"commands"`
	// Files are globs relative to the repository root. Writing files switches
	// from the ecosystem build to collecting what the commands produced.
	Files []string `toml:"files"`
}

// Requested reports whether the section turns asset handling on. An explicit
// enabled value wins; otherwise any setting turns it on.
func (a Assets) Requested() bool {
	if a.Enabled != nil {
		return *a.Enabled
	}
	return a.Name != "" || a.Main != "" || len(a.Targets) > 0 || len(a.Ldflags) > 0 || len(a.Commands) > 0 || len(a.Files) > 0
}

// Version configures files outside the package manifests that carry the
// release version. They are written by the version phase and checked by qegg
// check, so a Tauri app whose version lives in tauri.conf.json stays in step.
type Version struct {
	// File lists the extra files. The configuration spells each entry as a
	// [[version.file]] table.
	File []VersionFile `toml:"file"`
}

// VersionFile is one file outside the package manifests that carries the
// release version.
type VersionFile = versionfile.Spec

// Changes configures the change record directory and hook scope.
type Changes struct {
	Dir   string   `toml:"dir"`
	Scope []string `toml:"scope"`
}

// Verify configures commands that must pass before a version is bumped.
type Verify struct {
	Commands []string `toml:"commands"`
	Timeout  string   `toml:"timeout"`
	FailFast bool     `toml:"fail_fast"`
}

// Publish selects registries.
type Publish struct {
	Registries []string `toml:"registries"`
}

// Package registers one package in the repository. The map key in
// Config.Packages is the package identity; Name records the manifest's own
// name when the two differ (qegg init writes resolver-qualified keys).
type Package struct {
	Path      string   `toml:"path"`
	Resolver  string   `toml:"resolver"`
	Name      string   `toml:"name,omitempty"`
	Publish   *bool    `toml:"publish"`
	DependsOn []string `toml:"depends-on"`
}

// ManifestNameOf returns the manifest name of pkg, falling back to the key
// when the configuration does not set name explicitly.
func ManifestNameOf(key string, pkg Package) string {
	if name := strings.TrimSpace(pkg.Name); name != "" {
		return name
	}
	return key
}

// ManifestName returns the manifest name registered under key.
func (c *Config) ManifestName(key string) string {
	if pkg, ok := c.Packages[key]; ok {
		return ManifestNameOf(key, pkg)
	}
	return key
}

// PackageKeyFor resolves a package key or a unique manifest name to the
// configured key. It reports false when the value is unknown or matches more
// than one package.
func (c *Config) PackageKeyFor(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if _, ok := c.Packages[value]; ok {
		return value, true
	}
	var match string
	count := 0
	for _, key := range c.PackageNames() {
		if ManifestNameOf(key, c.Packages[key]) == value {
			match = key
			count++
		}
	}
	if count == 1 {
		return match, true
	}
	return "", false
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		SchemaVersion: SchemaVersion,
		Project:       Project{DefaultBranch: "main"},
		Git:           Git{TagPrefix: "v", Remote: "origin"},
		Release: Release{
			Mode:          ModePR,
			Branch:        "release",
			GitHubRelease: true,
			CommitMessage: "chore(release): v{{version}}",
			VersionScope:  VersionScopePackage,
			Workflow: Workflow{
				Mode:           WorkflowManaged,
				Runner:         DefaultWorkflowRunner,
				TimeoutMinutes: 60,
			},
		},
		Changes:  Changes{Dir: ".changes"},
		Verify:   Verify{Timeout: "15m", FailFast: true},
		Packages: map[string]Package{},
	}
}

// LoadFile reads and validates a configuration file.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	// Unknown keys are ignored on purpose: a configuration written by a newer
	// qegg must stay readable by an older binary that is still installed locally.
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// Load finds and loads the configuration starting at start.
func Load(start string) (*Config, string, error) {
	path, err := fsx.FindUp(start, FileName)
	if err != nil {
		return nil, "", ErrNotFound
	}
	cfg, err := LoadFile(path)
	if err != nil {
		return nil, "", err
	}
	return cfg, path, nil
}

func (c *Config) applyDefaults() {
	d := Default()
	if c.SchemaVersion == 0 {
		c.SchemaVersion = d.SchemaVersion
	}
	if c.Project.DefaultBranch == "" {
		c.Project.DefaultBranch = d.Project.DefaultBranch
	}
	if c.Git.TagPrefix == "" {
		c.Git.TagPrefix = d.Git.TagPrefix
	}
	if c.Git.Remote == "" {
		c.Git.Remote = d.Git.Remote
	}
	if c.Release.Mode == "" {
		c.Release.Mode = d.Release.Mode
	}
	if c.Release.Branch == "" {
		c.Release.Branch = d.Release.Branch
	}
	if c.Release.CommitMessage == "" {
		c.Release.CommitMessage = d.Release.CommitMessage
	}
	if c.Release.VersionScope == "" {
		c.Release.VersionScope = d.Release.VersionScope
	}
	if c.Release.Workflow.Mode == "" {
		c.Release.Workflow.Mode = d.Release.Workflow.Mode
	}
	if c.Release.Workflow.Runner == "" {
		c.Release.Workflow.Runner = d.Release.Workflow.Runner
	}
	if c.Release.Workflow.TimeoutMinutes == 0 {
		c.Release.Workflow.TimeoutMinutes = d.Release.Workflow.TimeoutMinutes
	}
	for i := range c.Release.Build {
		if c.Release.Build[i].Runner == "" {
			c.Release.Build[i].Runner = c.Release.Workflow.Runner
		}
		if c.Release.Build[i].TimeoutMinutes == 0 {
			c.Release.Build[i].TimeoutMinutes = c.Release.Workflow.TimeoutMinutes
		}
	}
	if c.Changes.Dir == "" {
		c.Changes.Dir = d.Changes.Dir
	}
	if c.Verify.Timeout == "" {
		c.Verify.Timeout = d.Verify.Timeout
	}
	if c.Packages == nil {
		c.Packages = map[string]Package{}
	}
}

// Validate checks invariants that do not require filesystem access.
func (c *Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (this qegg supports %d)", c.SchemaVersion, SchemaVersion)
	}
	if c.Release.Mode != ModePR && c.Release.Mode != ModeCommit {
		return fmt.Errorf("release.mode must be %q or %q, got %q", ModePR, ModeCommit, c.Release.Mode)
	}
	if c.Release.Mode == ModePR && c.Release.Branch == c.Project.DefaultBranch {
		return fmt.Errorf("release.branch must differ from project.default_branch %q in pr mode", c.Project.DefaultBranch)
	}
	switch c.Release.VersionScope {
	case VersionScopePackage, VersionScopeShared:
	default:
		return fmt.Errorf("release.version_scope must be %q or %q, got %q", VersionScopePackage, VersionScopeShared, c.Release.VersionScope)
	}
	if format := strings.TrimSpace(c.Git.TagFormat); format != "" {
		if !strings.Contains(format, "{{version}}") {
			return fmt.Errorf("git.tag_format %q must contain {{version}}", format)
		}
		unknown := format
		for _, placeholder := range []string{"{{version}}", "{{slug}}", "{{name}}"} {
			unknown = strings.ReplaceAll(unknown, placeholder, "")
		}
		if strings.Contains(unknown, "{{") {
			return fmt.Errorf("git.tag_format %q contains an unknown placeholder", format)
		}
	}
	for i, file := range c.Version.File {
		if err := file.Validate(); err != nil {
			return fmt.Errorf("version.file[%d]: %w", i, err)
		}
	}
	if len(c.Version.File) > 0 && len(c.Packages) > 1 && c.Release.VersionScope != VersionScopeShared {
		return errors.New("version.file needs release.version_scope = \"shared\" once several packages are registered")
	}
	if len(c.Packages) > 1 && c.Release.VersionScope != VersionScopeShared && c.Release.Assets.Requested() {
		return errors.New("release.assets needs release.version_scope = \"shared\" once several packages are registered")
	}
	if err := c.Release.Workflow.validate(c.Release.Build); err != nil {
		return err
	}
	if len(c.Release.Build) > 0 && len(c.Packages) > 1 && c.Release.VersionScope != VersionScopeShared {
		return errors.New("release.build needs release.version_scope = \"shared\" once several packages are registered")
	}
	if len(c.Release.Build) > 0 && c.Release.Assets.Enabled != nil && !*c.Release.Assets.Enabled {
		return errors.New("release.build uploads its artifacts; drop release.assets.enabled = false or the build jobs")
	}
	if c.Project.Repo != "" && strings.Count(c.Project.Repo, "/") < 2 {
		return fmt.Errorf("project.repo %q must look like host/owner/name", c.Project.Repo)
	}
	if _, err := time.ParseDuration(c.Verify.Timeout); err != nil {
		return fmt.Errorf("verify.timeout %q is not a valid duration", c.Verify.Timeout)
	}
	if err := c.Release.Assets.validate(); err != nil {
		return err
	}
	for name, pkg := range c.Packages {
		if strings.TrimSpace(pkg.Path) == "" {
			return fmt.Errorf("packages.%s.path must not be empty", name)
		}
		if strings.TrimSpace(pkg.Resolver) == "" {
			return fmt.Errorf("packages.%s.resolver must not be empty", name)
		}
		if pkg.Name != "" && strings.TrimSpace(pkg.Name) == "" {
			return fmt.Errorf("packages.%s.name must not be blank", name)
		}
	}
	return nil
}

// PackagePublishable reports whether a registered package publishes to a registry.
func (c *Config) PackagePublishable(name string) bool {
	pkg, ok := c.Packages[name]
	if !ok {
		return false
	}
	if pkg.Publish != nil {
		return *pkg.Publish
	}
	return true
}

// PackageNames returns registered package names sorted for deterministic output.
func (c *Config) PackageNames() []string {
	names := make([]string, 0, len(c.Packages))
	for name := range c.Packages {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

// validate checks the release asset settings that do not need the filesystem.
func (a Assets) validate() error {
	if strings.TrimSpace(a.Main) != "" {
		clean := path.Clean(filepath.ToSlash(strings.TrimSpace(a.Main)))
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("release.assets.main %q must stay inside the repository", a.Main)
		}
	}
	for _, target := range a.Targets {
		parts := strings.Split(strings.TrimSpace(target), "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("release.assets.targets contains invalid target %q", target)
		}
	}
	for _, pattern := range a.Files {
		if err := validateGlob("release.assets.files", pattern); err != nil {
			return err
		}
	}
	if len(a.Commands) > 0 && len(a.Files) == 0 {
		return errors.New("release.assets.commands needs at least one release.assets.files pattern")
	}
	if len(a.Files) > 0 && (a.Main != "" || len(a.Targets) > 0 || len(a.Ldflags) > 0) {
		return errors.New("release.assets.files cannot be combined with main, targets or ldflags")
	}
	for _, command := range a.Commands {
		if strings.TrimSpace(command) == "" {
			return errors.New("release.assets.commands must not contain an empty command")
		}
	}
	return nil
}

// buildJobNameRE allows the characters GitHub accepts in a job id.
var buildJobNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// validate checks the workflow settings that do not need the filesystem.
func (w Workflow) validate(builds []BuildJob) error {
	switch w.Mode {
	case WorkflowManaged, WorkflowExternal:
	default:
		return fmt.Errorf("release.workflow.mode must be %q or %q, got %q", WorkflowManaged, WorkflowExternal, w.Mode)
	}
	if strings.TrimSpace(w.Runner) == "" {
		return errors.New("release.workflow.runner must not be empty")
	}
	if w.TimeoutMinutes < 1 || w.TimeoutMinutes > 1440 {
		return fmt.Errorf("release.workflow.timeout_minutes %d must be between 1 and 1440", w.TimeoutMinutes)
	}
	if err := validateStepForms("release.workflow", w.Steps, w.YAML); err != nil {
		return err
	}
	if err := validateStepEnv("release.workflow.env", w.Env, w.YAML); err != nil {
		return err
	}
	if len(builds) > 0 && w.Mode != WorkflowManaged {
		return errors.New("release.build needs release.workflow.mode = \"managed\"")
	}
	seen := make(map[string]bool, len(builds))
	for i, job := range builds {
		name := strings.TrimSpace(job.Name)
		if name == "" {
			return fmt.Errorf("release.build[%d].name must not be empty", i)
		}
		if !buildJobNameRE.MatchString(name) {
			return fmt.Errorf("release.build[%d].name %q may only use letters, digits, dashes and underscores", i, job.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("release.build[%d].name %q is used twice", i, job.Name)
		}
		seen[key] = true
		if strings.TrimSpace(job.Runner) == "" {
			return fmt.Errorf("release.build[%d].runner must not be empty", i)
		}
		if len(job.Artifacts) == 0 {
			return fmt.Errorf("release.build[%d].artifacts must not be empty", i)
		}
		for _, artifact := range job.Artifacts {
			if err := validateGlob(fmt.Sprintf("release.build[%d].artifacts", i), artifact); err != nil {
				return err
			}
		}
		section := fmt.Sprintf("release.build[%d]", i)
		if err := validateStepForms(section, job.Steps, job.YAML); err != nil {
			return err
		}
		if err := validateStepEnv(section+".env", job.Env, job.YAML); err != nil {
			return err
		}
		if len(job.Steps) == 0 && strings.TrimSpace(job.YAML) == "" {
			return fmt.Errorf("%s needs steps or yaml", section)
		}
	}
	return nil
}

// validateStepForms checks that a step block is written exactly one way.
func validateStepForms(section string, steps []string, yaml string) error {
	if len(steps) > 0 && strings.TrimSpace(yaml) != "" {
		return fmt.Errorf("%s: steps and yaml are mutually exclusive", section)
	}
	for _, step := range steps {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("%s.steps must not contain an empty step", section)
		}
	}
	return nil
}

// validateStepEnv checks the environment variables of the shell steps. A raw
// yaml block carries its own env, so the field would be silently ignored.
func validateStepEnv(section string, env map[string]string, yaml string) error {
	if len(env) == 0 {
		return nil
	}
	if strings.TrimSpace(yaml) != "" {
		return fmt.Errorf("%s is only used by steps; set env inside the yaml block", section)
	}
	for key := range env {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s contains an empty variable name", section)
		}
	}
	return nil
}

// validateGlob rejects a configured glob that leaves the repository.
func validateGlob(section, pattern string) error {
	clean := path.Clean(filepath.ToSlash(strings.TrimSpace(pattern)))
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("%s %q must stay inside the repository", section, pattern)
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
