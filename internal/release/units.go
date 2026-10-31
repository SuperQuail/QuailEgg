package release

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
)

// defaultTagFormat renders tags for package-scoped multi-package repositories.
const defaultTagFormat = "{{slug}}-v{{version}}"

// tagSlug lowercases a package name and turns it into a tag component.
func tagSlug(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.NewReplacer("/", "-", "@", "-", " ", "-", "_", "-").Replace(slug)
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	return strings.Trim(slug, "-")
}

// renderTagFormat expands a tag format for one package.
func renderTagFormat(format, name, tagVersion string) string {
	rendered := strings.ReplaceAll(format, "{{version}}", tagVersion)
	rendered = strings.ReplaceAll(rendered, "{{slug}}", tagSlug(name))
	rendered = strings.ReplaceAll(rendered, "{{name}}", name)
	return rendered
}

// validateTag rejects tag names git refuses to create.
func validateTag(tag string) error {
	if tag == "" {
		return errors.New("the tag is empty")
	}
	if strings.HasPrefix(tag, "-") || strings.HasPrefix(tag, "/") || strings.HasPrefix(tag, "@") {
		return fmt.Errorf("tag %q must not start with a dash, slash or at sign", tag)
	}
	if strings.HasSuffix(tag, "/") || strings.HasSuffix(tag, ".lock") {
		return fmt.Errorf("tag %q must not end with a slash or .lock", tag)
	}
	if tag == "@" || strings.Contains(tag, "..") || strings.Contains(tag, "//") || strings.Contains(tag, "@{") {
		return fmt.Errorf("tag %q is not a valid git ref", tag)
	}
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_' || r == '/' || r == '@':
		default:
			return fmt.Errorf("tag %q contains %q, which git does not allow", tag, r)
		}
	}
	return nil
}

// packageTag renders the tag of one package. A single package and a
// shared-version repository keep the tag_prefix form; a package-scoped
// multi-package repository uses tag_format.
func packageTag(cfg *config.Config, scope string, pkg ecosystem.Package, version string) (string, error) {
	if scope != config.VersionScopeShared && len(cfg.Packages) > 1 {
		format := strings.TrimSpace(cfg.Git.TagFormat)
		if format == "" {
			format = defaultTagFormat
		}
		tag := renderTagFormat(format, pkg.IdentityName(), registry.TagVersion(pkg.Resolver, version))
		if err := validateTag(tag); err != nil {
			return "", err
		}
		return tag, nil
	}
	return tagName(cfg, pkg.Resolver, version), nil
}

// TagFor renders the tag of one package for the configured version scope.
func TagFor(cfg *config.Config, scope string, pkg ecosystem.Package, version string) (string, error) {
	return packageTag(cfg, scope, pkg, version)
}
