package release

import (
	"context"
	"errors"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// LedgerEntryFor reads the publish record of the release the committed marker
// describes. It is what qegg status reports: which package the original run
// already published, which one it skipped on purpose and why. A nil entry means
// no record exists, which is the state of a release started by an older qegg or
// of a release whose publish phase never ran.
//
// remote=false reads only the local copy, so qegg status --offline stays offline.
func LedgerEntryFor(ctx context.Context, d *Deps, remote bool) (*state.LedgerEntry, error) {
	marker, err := state.LoadMarker(d.Root)
	if err != nil || marker == nil {
		return nil, err
	}
	repo, _ := github.Detect(ctx, d.Run, d.Root, d.Cfg.Git.Remote)
	name, err := repoName(ctx, d, repo)
	if err != nil {
		return nil, err
	}
	identity, err := markerIdentity(d.Cfg, marker, name, d.Cfg.Release.Mode)
	if err != nil {
		return nil, err
	}
	local := state.LocalLedgerStore{Root: d.Root}
	if localLedger, _, localErr := local.Load(); localErr == nil {
		if entry := localLedger.Find(identity); entry != nil {
			// The local mirror is the fast, offline-safe answer. Only a runner that
			// never ran this release has to read the remote record.
			return entry, nil
		}
	}
	var store state.LedgerStore = local
	if remote && d.Cfg.Git.Remote != "" {
		if url, urlErr := remoteURL(ctx, d.Run, d.Root, d.Cfg.Git.Remote); urlErr == nil && url != "" {
			if gitStore, storeErr := NewGitLedgerStore(d.Run, d.Root, d.Cfg.Git.Remote, url, ledgerRefTag(identity)); storeErr == nil {
				store = gitStore
			}
		}
	}
	ledger, _, err := store.Load()
	if err != nil {
		if store == state.LedgerStore(local) {
			// A corrupt local copy is worth reporting; an unreachable remote is not,
			// because qegg status is a diagnostic that must still work offline.
			if errors.Is(err, state.ErrLedgerFormat) {
				return nil, errors.New(i18n.Tf("release.err.ledger_format", store.Describe()))
			}
			return nil, errors.New(i18n.Tf("release.err.ledger_read", store.Describe(), err))
		}
		return nil, nil
	}
	entry := ledger.Find(identity)
	if entry != nil && entry.Identity.SourceCommit == "" {
		entry.Identity.SourceCommit = identity.SourceCommit
	}
	return entry, nil
}

// markerIdentity derives the release identity from the committed marker, which
// is the identity every run of that release computes: a resume, a repair and
// qegg status all have to land on the same ledger entry.
func markerIdentity(cfg *config.Config, marker *state.Marker, repo, mode string) (state.ReleaseIdentity, error) {
	releases := markerReleasePlansFor(cfg, marker, configuredNames(cfg), configuredResolver(cfg))
	channel := marker.Channel
	if channel == "" {
		for _, release := range releases {
			if release.Prerelease != "" {
				channel = release.Prerelease
				break
			}
		}
	}
	return state.ReleaseIdentity{
		SchemaVersion: 1,
		Repo:          repo,
		Tags:          releaseTags(releases),
		Version:       marker.Version,
		Channel:       channel,
		Mode:          mode,
		Fingerprint:   marker.Fingerprint,
	}, nil
}

// repoName names the repository in the ledger. The hosting identity is preferred
// because it survives a new clone; the remote URL is the fallback.
func repoName(ctx context.Context, d *Deps, repo github.Repo) (string, error) {
	if repo.Valid() {
		return repo.ID(), nil
	}
	if d.Cfg.Git.Remote != "" {
		if url, err := remoteURL(ctx, d.Run, d.Root, d.Cfg.Git.Remote); err == nil && url != "" {
			return url, nil
		}
	}
	return "local:" + d.Root, nil
}
