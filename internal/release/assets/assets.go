// Package assets builds and packages the binaries attached to a release.
package assets

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// Target is one GOOS/GOARCH pair.
type Target struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// String renders the target as os/arch.
func (t Target) String() string { return t.OS + "/" + t.Arch }

// Options configures detection. Zero values mean "use the defaults".
type Options struct {
	Enabled *bool
	Name    string
	Main    string
	Targets []string
	Ldflags []string
}

// Plan describes the assets of one release.
type Plan struct {
	Name    string   `json:"name"`
	Main    string   `json:"main"`
	Targets []Target `json:"targets"`
	Ldflags []string `json:"-"`
}

// DefaultTargets is the platform matrix used when none is configured.
var DefaultTargets = []string{
	"linux/amd64",
	"linux/arm64",
	"darwin/amd64",
	"darwin/arm64",
	"windows/amd64",
}

// DefaultLdflags injects the release version into a conventional main.version
// variable. The linker ignores the flag when the variable does not exist.
var DefaultLdflags = []string{"-X main.version={{version}}"}

// Detect resolves the main package and the asset plan of a Go module. It
// returns a nil plan when assets are disabled or the module has no main
// package, plus non-fatal warnings for the caller to surface.
func Detect(root string, opts Options) (*Plan, []string, error) {
	if !enabled(opts) {
		return nil, nil, nil
	}
	main, name, warnings, err := resolveMain(root, opts)
	if err != nil {
		return nil, nil, err
	}
	if main == "" {
		return nil, warnings, nil
	}
	targets, err := ParseTargets(opts.Targets)
	if err != nil {
		return nil, nil, err
	}
	ldflags := opts.Ldflags
	if len(ldflags) == 0 {
		ldflags = DefaultLdflags
	}
	if err := validateLdflags(ldflags); err != nil {
		return nil, nil, err
	}
	return &Plan{Name: name, Main: main, Targets: targets, Ldflags: ldflags}, warnings, nil
}

// enabled reports whether release assets are requested. They are off by
// default; writing any asset setting turns them on, and an explicit enabled
// value wins.
func enabled(opts Options) bool {
	if opts.Enabled != nil {
		return *opts.Enabled
	}
	return opts.Name != "" || opts.Main != "" || len(opts.Targets) > 0 || len(opts.Ldflags) > 0
}

func resolveMain(root string, opts Options) (main, name string, warnings []string, err error) {
	if opts.Main != "" {
		dir, err := safeRelative(opts.Main)
		if err != nil {
			return "", "", nil, err
		}
		if !isMainPackage(filepath.Join(root, filepath.FromSlash(dir))) {
			return "", "", nil, errors.New(i18n.Tf("release.err.assets_main", opts.Main))
		}
		return dir, chooseName(opts.Name, root, dir), nil, nil
	}
	candidates, err := cmdMainPackages(root)
	if err != nil {
		return "", "", nil, err
	}
	switch len(candidates) {
	case 1:
		dir := candidates[0]
		return dir, chooseName(opts.Name, root, dir), nil, nil
	case 0:
		if isMainPackage(root) {
			return ".", chooseName(opts.Name, root, "."), nil, nil
		}
		return "", "", nil, nil
	default:
		if opts.Enabled != nil && *opts.Enabled {
			return "", "", nil, errors.New(i18n.T("release.err.assets_ambiguous"))
		}
		return "", "", []string{i18n.T("release.warn.assets_ambiguous")}, nil
	}
}

func chooseName(configured, root, dir string) string {
	if configured != "" {
		return configured
	}
	if dir == "." || dir == "" {
		return filepath.Base(filepath.Clean(root))
	}
	return path.Base(dir)
}

// cmdMainPackages lists the immediate subdirectories of cmd/ that are main
// packages.
func cmdMainPackages(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := path.Join("cmd", entry.Name())
		if isMainPackage(filepath.Join(root, filepath.FromSlash(dir))) {
			out = append(out, dir)
		}
	}
	return out, nil
}

// isMainPackage reports whether dir contains a non-test Go file that declares
// package main.
func isMainPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		if file.Name != nil && file.Name.Name == "main" {
			return true
		}
	}
	return false
}

// ParseTargets parses os/arch pairs, applying the default matrix when empty.
func ParseTargets(values []string) ([]Target, error) {
	raw := values
	if len(raw) == 0 {
		raw = DefaultTargets
	}
	seen := make(map[string]bool, len(raw))
	out := make([]Target, 0, len(raw))
	for _, value := range raw {
		parts := strings.Split(strings.TrimSpace(value), "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New(i18n.Tf("release.err.assets_target", value))
		}
		osName := strings.TrimSpace(parts[0])
		arch := strings.TrimSpace(parts[1])
		key := osName + "/" + arch
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Target{OS: osName, Arch: arch})
	}
	return out, nil
}

var placeholderRE = regexp.MustCompile(`\{\{([a-zA-Z0-9_.]+)\}\}`)

func validateLdflags(flags []string) error {
	for _, flag := range flags {
		for _, match := range placeholderRE.FindAllStringSubmatch(flag, -1) {
			switch match[1] {
			case "version", "tag":
			default:
				return errors.New(i18n.Tf("release.err.assets_placeholder", match[0]))
			}
		}
	}
	return nil
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

// VersionParts splits a version into its plain and v-prefixed forms.
func VersionParts(version string) (plain, tag string) {
	plain = strings.TrimPrefix(strings.TrimSpace(version), "v")
	return plain, "v" + plain
}

func safeRelative(value string) (string, error) {
	clean := path.Clean(filepath.ToSlash(strings.TrimSpace(value)))
	if clean == "." {
		return ".", nil
	}
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New(i18n.Tf("release.err.assets_path", value))
	}
	return clean, nil
}
