// Package config loads and validates .quailegg.toml.
package config

import (
	"errors"
	"fmt"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path"
	"path/filepath"
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
}

// Assets configures the binaries attached to a GitHub release. Only Go modules
// are supported for now; an empty section keeps the automatic defaults.
type Assets struct {
	Enabled *bool    `toml:"enabled"`
	Name    string   `toml:"name"`
	Main    string   `toml:"main"`
	Targets []string `toml:"targets"`
	Ldflags []string `toml:"ldflags"`
}

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

// Package registers one package in the repository.
type Package struct {
	Path      string   `toml:"path"`
	Resolver  string   `toml:"resolver"`
	Publish   *bool    `toml:"publish"`
	DependsOn []string `toml:"depends-on"`
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
	if len(c.Packages) > 1 {
		if c.Release.VersionScope == VersionScopeShared {
			resolver := ""
			for _, pkg := range c.Packages {
				if pkg.Resolver == ResolverGo {
					return errors.New("release.version_scope shared does not support Go modules")
				}
				if resolver == "" {
					resolver = pkg.Resolver
				} else if pkg.Resolver != resolver {
					return fmt.Errorf("release.version_scope shared requires one resolver, found %q and %q", resolver, pkg.Resolver)
				}
			}
		}
		assets := c.Release.Assets
		if assets.Enabled != nil || assets.Name != "" || assets.Main != "" || len(assets.Targets) > 0 || len(assets.Ldflags) > 0 {
			return errors.New("release.assets does not support multi-package repositories")
		}
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
		switch pkg.Resolver {
		case ResolverNPM, ResolverPython, ResolverRust, ResolverGo:
		default:
			return fmt.Errorf("packages.%s.resolver %q is not supported", name, pkg.Resolver)
		}
	}
	for _, registry := range c.Publish.Registries {
		switch registry {
		case ResolverNPM, ResolverGo, "pypi", "crates":
		default:
			return fmt.Errorf("publish.registries contains unknown registry %q", registry)
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
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
