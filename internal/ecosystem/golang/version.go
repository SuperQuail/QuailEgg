package golang

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	mastersemver "github.com/Masterminds/semver/v3"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// rootOnly guards the tag rules: only the module at the repository root can be
// released, because subdirectory modules need sub/vX.Y.Z tags.
func rootOnly(pkg ecosystem.Package) error {
	if pkg.Path != "." {
		return fmt.Errorf("go module %s: only the module at the repository root is supported", pkg.Name)
	}
	return nil
}

// SetVersion is a no-op: a Go module version is its release tag, which the
// release pipeline creates after the version commit.
func (driver) SetVersion(_ string, pkg ecosystem.Package, _ string) error {
	return rootOnly(pkg)
}

// CurrentVersion returns the highest release tag reachable from HEAD, or 0.0.0
// when the repository has no release tag yet.
func (driver) CurrentVersion(ctx context.Context, run execx.Runner, root string, pkg ecosystem.Package, tagPrefix string) (string, error) {
	if err := rootOnly(pkg); err != nil {
		return "", err
	}
	if tagPrefix == "" {
		tagPrefix = "v"
	}
	res, err := run.Run(ctx, root, "git", "tag", "--merged", "HEAD", "--list", tagPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("list release tags: %w", err)
	}
	best := mastersemver.Version{}
	found := false
	for _, line := range strings.Split(res.Stdout, "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" || !strings.HasPrefix(tag, tagPrefix) {
			continue
		}
		parsed, err := semver.Parse(strings.TrimPrefix(tag, tagPrefix))
		if err != nil {
			continue
		}
		if !found || parsed.GreaterThan(&best) {
			best = parsed
			found = true
		}
	}
	if !found {
		return "0.0.0", nil
	}
	return best.String(), nil
}

// CheckVersion rejects versions that the module path cannot express: the Go
// toolchain maps v2 and later to a /vN path suffix.
func (driver) CheckVersion(pkg ecosystem.Package, next string) error {
	parsed, err := semver.Parse(next)
	if err != nil {
		return err
	}
	major := int(parsed.Major())
	want := modulePathForMajor(pkg.Name, major)
	if want == pkg.Name {
		return nil
	}
	return errors.New(i18n.Tf("release.err.go_major", pkg.Name, next, want))
}

var majorSuffix = regexp.MustCompile("/v([0-9]+)$")

// modulePathForMajor returns the module path Go requires for the given major
// version: /v2+ for v2 and later, no suffix for v0 and v1.
func modulePathForMajor(path string, major int) string {
	base := path
	if match := majorSuffix.FindStringSubmatch(path); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil && n >= 2 {
			base = strings.TrimSuffix(path, match[0])
		}
	}
	if major >= 2 {
		return base + "/v" + strconv.Itoa(major)
	}
	return base
}
