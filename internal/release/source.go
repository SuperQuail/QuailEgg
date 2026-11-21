package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// This file pins the exact commit a release builds, uploads and tags from.
//
// The version phase writes the marker and then creates the commit that carries
// it, so a marker can never name its own commit. The snapshot is therefore
// recovered from the record qegg wrote once the commit existed, or from the
// branch history by matching the marker content. Either way it is pinned before
// the first external write, and every build, upload and tag then uses that
// commit even when the checked-out branch has moved on.
//
// A marker that no commit carries is never guessed: the release would tag a
// tree without the version bump, so it stops instead.

// LocateSource pins the commit the release is built from. It looks at, in
// order: the record qegg wrote for this release identity, then the commits in
// the first-parent history of HEAD that introduced the marker this tree holds.
// Zero matches, several matches, an uncommitted marker and a shallow clone all
// stop the release: a snapshot that cannot be named must not be published.
func LocateSource(ctx context.Context, d *Deps, id state.ReleaseIdentity) (state.ReleaseIdentity, error) {
	marker, err := state.LoadMarker(d.Root)
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	if marker == nil {
		return state.ReleaseIdentity{}, errors.New(i18n.T("release.err.no_marker"))
	}
	token := markerToken(marker)
	recorded, ok, err := recordedSource(ctx, d, id, token)
	if err != nil || ok {
		return recorded, err
	}
	headToken, headHasMarker, err := tokenAt(ctx, d, "HEAD")
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	if !headHasMarker || headToken != token {
		// The marker exists only in the working tree. Tooling cannot point a tag
		// at a tree that does not contain it, so this is a stop, not a warning.
		return state.ReleaseIdentity{}, errors.New(i18n.Tf("release.err.source_uncommitted", describeIdentity(id)))
	}
	shallow, err := d.Git.IsShallow(ctx)
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	if shallow {
		// A shallow clone cuts the history at its first commit, so the commits
		// it does have all look like the one that introduced the marker, and the
		// parent test below would agree with the wrong commit. Refusing is the
		// only honest answer; guessing would build from the tree that moved on.
		return state.ReleaseIdentity{}, errors.New(i18n.Tf("release.err.source_shallow", describeIdentity(id)))
	}
	matches, err := snapshotCommits(ctx, d, token)
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	switch len(matches) {
	case 1:
		return recordSource(ctx, d, id, matches[0], state.SourceOriginHistory)
	case 0:
		// HEAD carries the marker but the commit that introduced it is not in
		// the local history: the snapshot cannot be named.
		return state.ReleaseIdentity{}, errors.New(i18n.Tf("release.err.source_missing", describeIdentity(id)))
	default:
		return state.ReleaseIdentity{}, errors.New(i18n.Tf("release.err.source_ambiguous", describeIdentity(id), shortList(matches)))
	}
}

// PinSource adopts a commit the caller supplied and checks that it carries the
// marker of this release.
func PinSource(ctx context.Context, d *Deps, id state.ReleaseIdentity, rev string) (state.ReleaseIdentity, error) {
	commit, err := d.Git.ResolveCommit(ctx, rev)
	if err != nil {
		return state.ReleaseIdentity{}, errors.New(i18n.Tf("release.err.source_unknown", rev))
	}
	marker, err := state.LoadMarker(d.Root)
	if err != nil {
		return state.ReleaseIdentity{}, err
	}
	if marker != nil {
		want := markerToken(marker)
		got, hasMarker, err := tokenAt(ctx, d, commit)
		if err != nil {
			return state.ReleaseIdentity{}, err
		}
		if !hasMarker || got != want {
			return state.ReleaseIdentity{}, errors.New(i18n.Tf("release.err.source_mismatch", shortSHA(commit), describeIdentity(id)))
		}
	}
	return recordSource(ctx, d, id, commit, state.SourceOriginPinned)
}

// recordedSource reads the snapshot qegg recorded for this release identity.
func recordedSource(ctx context.Context, d *Deps, id state.ReleaseIdentity, token string) (state.ReleaseIdentity, bool, error) {
	record, err := state.LoadSource(d.Root, id.Key())
	if err != nil || record == nil || record.Commit == "" {
		return state.ReleaseIdentity{}, false, err
	}
	if !record.Identity.SameRelease(id) {
		// A record for another release intent cannot name this snapshot.
		return state.ReleaseIdentity{}, false, nil
	}
	commit, err := d.Git.ResolveCommit(ctx, record.Commit)
	if err != nil {
		// The recorded commit is gone (rewritten history, collected objects):
		// fall back to the history lookup instead of failing.
		return state.ReleaseIdentity{}, false, nil
	}
	reachable, err := d.Git.IsAncestor(ctx, commit, "HEAD")
	if err != nil {
		return state.ReleaseIdentity{}, false, err
	}
	if !reachable {
		// A recorded commit that is not on this branch cannot be the snapshot
		// this branch publishes, whatever it contains.
		return state.ReleaseIdentity{}, false, nil
	}
	got, hasMarker, err := tokenAt(ctx, d, commit)
	if err != nil {
		return state.ReleaseIdentity{}, false, err
	}
	if !hasMarker || got != token {
		return state.ReleaseIdentity{}, false, errors.New(i18n.Tf("release.err.source_mismatch", shortSHA(commit), describeIdentity(id)))
	}
	return id.WithSource(commit), true, nil
}

// recordSource stores the snapshot for the release identity and returns it.
func recordSource(ctx context.Context, d *Deps, id state.ReleaseIdentity, commit, origin string) (state.ReleaseIdentity, error) {
	pinned := id.WithSource(commit)
	now := d.Now
	if now == nil {
		now = time.Now
	}
	record := &state.SourceRecord{
		SchemaVersion: 1,
		Key:           pinned.Key(),
		Identity:      pinned,
		Commit:        commit,
		Origin:        origin,
		RecordedAt:    now(),
	}
	if err := state.SaveSource(d.Root, record); err != nil {
		return state.ReleaseIdentity{}, err
	}
	if d.UI != nil {
		d.UI.Info("%s", i18n.Tf("release.msg.source", shortSHA(commit), sourceOriginLabel(origin)))
	}
	return pinned, nil
}

// sourceOriginLabel renders where the pinned snapshot came from.
func sourceOriginLabel(origin string) string {
	switch origin {
	case state.SourceOriginCommit:
		return i18n.T("release.source.recorded")
	case state.SourceOriginPinned:
		return i18n.T("release.source.pinned")
	default:
		return i18n.T("release.source.history")
	}
}

// snapshotCommits lists the commits that introduced the marker, newest first.
// Only the first-parent history counts: a release branch merged into the target
// branch leaves its own commit behind the merge, and the merge is the commit the
// target branch holds.
//
// A commit introduces the marker when its parent does not carry the same marker
// content yet. Without that test any later commit that rewrote the marker to the
// same intent would match as well, and rewriting it twice is exactly the case
// that must not be published on a guess.
func snapshotCommits(ctx context.Context, d *Deps, token string) ([]string, error) {
	commits, err := d.Git.CommitsTouching(ctx, state.MarkerRelPath())
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, commit := range commits {
		ok, err := introducesMarker(ctx, d, commit, token)
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, commit)
		}
	}
	return matches, nil
}

// introducesMarker reports whether commit carries the marker while its first
// parent does not.
func introducesMarker(ctx context.Context, d *Deps, commit, token string) (bool, error) {
	got, ok, err := candidateToken(ctx, d, commit)
	if err != nil || !ok {
		return false, err
	}
	if got != token {
		return false, nil
	}
	parent, hasParent, err := d.Git.ParentCommit(ctx, commit)
	if err != nil {
		return false, err
	}
	if !hasParent {
		return true, nil
	}
	parentToken, _, err := candidateToken(ctx, d, parent)
	if err != nil {
		return false, err
	}
	return parentToken != token, nil
}

// candidateToken reads the marker token of a commit while walking the history. A
// marker that cannot be parsed drops the commit out of the candidates instead of
// failing the whole lookup: one broken marker in the past must not make every
// later release unbuildable.
func candidateToken(ctx context.Context, d *Deps, rev string) (string, bool, error) {
	token, found, err := tokenAt(ctx, d, rev)
	if err != nil {
		return "", false, nil
	}
	return token, found && token != "", nil
}

// tokenAt returns the marker token of a commit. found is false when the commit
// carries no marker; a marker that cannot be read is an error, so a caller that
// must be right about one commit is not quietly told "no marker".
func tokenAt(ctx context.Context, d *Deps, rev string) (string, bool, error) {
	data, found, err := d.Git.FileAtCommit(ctx, rev, state.MarkerRelPath())
	if err != nil || !found {
		return "", false, err
	}
	var marker state.Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return "", false, fmt.Errorf("%s: %w", i18n.Tf("release.err.marker_unreadable", state.MarkerRelPath(), shortSHA(rev)), err)
	}
	return markerToken(&marker), true, nil
}

// markerToken identifies marker content: the fingerprint when the marker has
// one, otherwise a digest of the normalised marker, so a marker written before
// fingerprints existed still matches the commit that introduced it.
func markerToken(marker *state.Marker) string {
	if marker == nil {
		return ""
	}
	if marker.Fingerprint != "" {
		return "fingerprint:" + marker.Fingerprint
	}
	// Marshalling the struct normalises whitespace and key order, which is what
	// makes the digest comparable between the working tree and a commit.
	data, err := json.Marshal(marker)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "content:" + hex.EncodeToString(sum[:])
}

// sameSourceCommit reports whether two hashes name the same commit, tolerating
// an abbreviated hash. The name is specific to this file so a repair path that
// needs its own comparison does not collide with it.
func sameSourceCommit(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	if len(a) < 7 || len(b) < 7 {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// shortList renders commit hashes for an error message.
func shortList(commits []string) string {
	short := make([]string, 0, len(commits))
	for _, commit := range commits {
		short = append(short, shortSHA(commit))
	}
	if len(short) > 3 {
		short = append(short[:3], "...")
	}
	return strings.Join(short, ", ")
}

// resolveSource pins the release snapshot before the first external write, then
// checks the remote tags it is about to rely on and reads the notes of that
// snapshot.
func (e *executor) resolveSource(ctx context.Context) error {
	if err := e.pinSource(ctx); err != nil {
		return err
	}
	source := e.identity.SourceCommit
	if err := e.checkRemoteTags(ctx, source); err != nil {
		return err
	}
	e.changelog = e.readChangelog(ctx, source)
	return nil
}

// pinSource resolves and stores the snapshot without the follow-up checks.
func (e *executor) pinSource(ctx context.Context) error {
	if e.identity.SourceCommit != "" && strings.TrimSpace(e.d.Options.SourceCommit) == "" {
		// The version phase already pinned the commit it created; the same run
		// keeps it instead of looking it up again.
		e.run.SourceCommit = e.identity.SourceCommit
		return nil
	}
	id, err := IdentityFor(e.d, e.plan)
	if err != nil {
		return err
	}
	if pinned := strings.TrimSpace(e.d.Options.SourceCommit); pinned != "" {
		id, err = PinSource(ctx, e.d, id, pinned)
	} else {
		id, err = LocateSource(ctx, e.d, id)
	}
	if err != nil {
		return err
	}
	e.identity = id
	e.run.SourceCommit = id.SourceCommit
	return nil
}

// readChangelog returns CHANGELOG.md as the pinned snapshot has it, so the
// release notes describe the tree that was published. A snapshot without the
// file keeps the working-tree copy for compatibility.
func (e *executor) readChangelog(ctx context.Context, commit string) []byte {
	if commit != "" {
		if data, found, err := e.d.Git.FileAtCommit(ctx, commit, "CHANGELOG.md"); err == nil && found {
			return data
		}
	}
	data, err := os.ReadFile(filepath.Join(e.d.Root, "CHANGELOG.md"))
	if err != nil {
		return nil
	}
	return data
}

// sourceRoot returns the directory the release is built from: the working tree
// when HEAD is the snapshot, an isolated checkout of the snapshot otherwise.
// Building from a branch that moved is exactly what the snapshot prevents.
func (e *executor) sourceRoot(ctx context.Context) (string, error) {
	if e.snapshot != "" {
		return e.snapshot, nil
	}
	commit := e.identity.SourceCommit
	if commit == "" {
		return "", errors.New(i18n.T("release.err.source_unset"))
	}
	head, err := e.d.Git.HeadSHA(ctx)
	if err != nil {
		return "", err
	}
	if sameSourceCommit(head, commit) {
		if err := e.requireCleanSnapshot(ctx); err != nil {
			return "", err
		}
		e.snapshot = e.d.Root
		return e.snapshot, nil
	}
	parent, err := os.MkdirTemp("", "qegg-source-")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(parent, "src")
	if err := e.d.Git.AddWorktree(ctx, dir, commit); err != nil {
		os.RemoveAll(parent)
		return "", errors.New(i18n.Tf("release.err.source_snapshot", shortSHA(commit), err))
	}
	e.snapshot = dir
	e.snapshotRoot = parent
	e.d.UI.Info("%s", i18n.Tf("release.msg.source_snapshot", shortSHA(commit)))
	return dir, nil
}

// requireCleanSnapshot refuses to build from a checked-out tree that carries
// changes: the tree then describes something the release commit never had. The
// publish phase has no preflight of its own, so this is the only guard between a
// dirty resume and a build from it.
func (e *executor) requireCleanSnapshot(ctx context.Context) error {
	if e.d.Options.AllowDirty {
		return nil
	}
	// The release marker and the runtime checkpoints are written by qegg
	// itself; anything else is a change the build would pick up.
	exclude := append(state.RuntimePaths(), state.MarkerRelPath())
	clean, err := e.d.Git.IsCleanExcept(ctx, exclude)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New(i18n.T("release.err.source_dirty"))
	}
	return nil
}

// workingTreeIsSource reports whether the checked-out tree is the release
// snapshot. It is false exactly when an isolated checkout was needed, which is
// also when a prebuilt assets directory must prove its source commit.
func (e *executor) workingTreeIsSource() bool {
	return e.snapshot == "" || e.snapshot == e.d.Root
}

// releaseSnapshot removes the isolated checkout of the release snapshot.
func (e *executor) releaseSnapshot(ctx context.Context) {
	if e.snapshotRoot == "" {
		return
	}
	// Cleanup must still run when the run was cancelled or failed, so it gets a
	// context that is not cancelled with the release.
	cleanup := context.WithoutCancel(ctx)
	if err := e.d.Git.RemoveWorktree(cleanup, e.snapshot); err != nil {
		e.d.UI.Warn("%s", i18n.Tf("release.warn.source_snapshot", err))
		e.d.Git.PruneWorktrees(cleanup)
	}
	os.RemoveAll(e.snapshotRoot)
	e.snapshot, e.snapshotRoot = "", ""
}
