// Package config loads and validates .quailegg.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
	"github.com/pelletier/go-toml/v2"
	"os"
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
}

// Git holds git integration settings.
type Git struct {
	TagPrefix string `toml:"tag_prefix"`
	Remote    string `toml:"remote"`
}

// Release controls how versions are produced and published.
type Release struct {
	Mode          string `toml:"mode"`
	Branch        string `toml:"branch"`
	GitHubRelease bool   `toml:"github_release"`
	CommitMessage string `toml:"commit_message"`
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
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
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
	if _, err := time.ParseDuration(c.Verify.Timeout); err != nil {
		return fmt.Errorf("verify.timeout %q is not a valid duration", c.Verify.Timeout)
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

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
