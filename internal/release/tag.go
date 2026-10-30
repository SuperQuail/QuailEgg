package release

import (
	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// tagName renders the git tag for a version: the ecosystem's tag spelling comes
// first, the configured prefix second.
func tagName(cfg *config.Config, resolver, version string) string {
	return semver.Tag(cfg.Git.TagPrefix, registry.TagVersion(resolver, version))
}
