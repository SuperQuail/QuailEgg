// Package assets plans, builds and collects the files attached to a release.
package assets

import (
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// Target is one os/arch pair of a self-built asset plan.
type Target = ecosystem.AssetTarget

// Plan describes the assets of one release. A plan with a main package
// cross-compiles a Go module; a plan with files runs the configured commands
// and collects what they produced.
type Plan struct {
	Name     string   `json:"name"`
	Main     string   `json:"main,omitempty"`
	Targets  []Target `json:"targets,omitempty"`
	Ldflags  []string `json:"-"`
	Commands []string `json:"commands,omitempty"`
	Files    []string `json:"files,omitempty"`
}

// SelfBuilt reports whether the plan has an ecosystem build behind it.
func (p *Plan) SelfBuilt() bool { return p.Main != "" }

// Validate checks the plan before anything runs.
func (p *Plan) Validate() error {
	if len(p.Files) == 0 {
		if len(p.Commands) > 0 {
			return errors.New(i18n.T("release.err.assets_commands_without_files"))
		}
		return nil
	}
	for _, command := range p.Commands {
		if strings.TrimSpace(command) == "" {
			return errors.New(i18n.T("release.err.assets_empty_command"))
		}
	}
	for _, pattern := range p.Files {
		clean := path.Clean(filepath.ToSlash(strings.TrimSpace(pattern)))
		if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return errors.New(i18n.Tf("release.err.assets_files", pattern))
		}
	}
	return nil
}

// VersionParts splits a version into its plain and v-prefixed forms.
func VersionParts(version string) (plain, tag string) {
	plain = strings.TrimPrefix(strings.TrimSpace(version), "v")
	return plain, "v" + plain
}

// expand substitutes the version placeholders of the ldflags.
func expand(flags []string, version string) []string {
	plain, tag := VersionParts(version)
	replacer := strings.NewReplacer("{{version}}", plain, "{{tag}}", tag)
	out := make([]string, 0, len(flags))
	for _, flag := range flags {
		out = append(out, replacer.Replace(flag))
	}
	return out
}
