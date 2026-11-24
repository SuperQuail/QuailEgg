package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

// LedgerRefPrefix names the ref that carries the release ledger of one tag on the
// origin remote. It is not a branch and not a tag: normal clones, fetches and git
// status never see it, but any runner with push access reads it, which is what
// makes the record survive a change of CI runner.
const LedgerRefPrefix = "refs/qegg/ledger/"

// LedgerPathInRef is the ledger file inside the ledger commit.
const LedgerPathInRef = ".quailegg/publish-ledger.json"

// GitLedgerStore keeps the ledger in a git commit under its release ref.
// Every write is a compare-and-swap: the push is accepted only while the remote
// ref still holds the revision this run read, so two concurrent resumes cannot
// both record the same package as "started".
type GitLedgerStore struct {
	Run    execx.Runner
	Remote string
	// Dir is the working directory the git commands run in.
	Dir string

	url string
	tag string
}

// NewGitLedgerStore binds the ledger store to one remote URL and one release tag.
func NewGitLedgerStore(run execx.Runner, dir, remote, url, tag string) (*GitLedgerStore, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("%s", i18n.Tf("release.err.ledger_remote", remote))
	}
	if strings.TrimSpace(tag) == "" {
		tag = "default"
	}
	return &GitLedgerStore{Run: run, Remote: remote, Dir: dir, url: url, tag: tag}, nil
}

// Ref returns the ref this store reads and writes.
func (s *GitLedgerStore) Ref() string { return LedgerRefPrefix + s.tag }

// Describe implements state.LedgerStore.
func (s *GitLedgerStore) Describe() string { return s.Remote + " " + s.Ref() }

// Load implements state.LedgerStore. A missing ref is an empty ledger, not an
// error: the caller decides whether that counts as evidence.
func (s *GitLedgerStore) Load() (*state.Ledger, string, error) {
	ctx := context.Background()
	sha, err := s.remoteRef(ctx)
	if err != nil {
		return nil, "", err
	}
	if sha == "" {
		return state.NewLedger(), "", nil
	}
	data, err := s.readFile(ctx, sha)
	if err != nil {
		return nil, "", err
	}
	ledger, err := state.ParseLedger(data)
	if err != nil {
		return nil, "", err
	}
	return ledger, sha, nil
}

// Save implements state.LedgerStore.
func (s *GitLedgerStore) Save(ledger *state.Ledger, token string) (string, error) {
	ctx := context.Background()
	commit, err := s.writeCommit(ctx, ledger)
	if err != nil {
		return "", err
	}
	if commit == token {
		// The commit hash repeats the revision this write expected to replace, so
		// the push would be a no-op that reports success. Treat it as a lost
		// compare-and-swap instead of pretending the ledger moved.
		return "", state.ErrLedgerConflict
	}
	lease := "--force-with-lease=" + s.Ref() + ":" + token
	_, pushErr := s.output(ctx, "push", lease, "--", s.url, commit+":"+s.Ref())
	if pushErr == nil {
		return commit, nil
	}
	// A rejected push is either a compare-and-swap loss or a real failure.
	// Ask the remote which one it is instead of matching error text.
	current, err := s.remoteRef(ctx)
	if err != nil {
		return "", pushErr
	}
	if current != token {
		return "", state.ErrLedgerConflict
	}
	return "", pushErr
}

// ledgerCommitSeq makes every ledger commit unique. A commit timestamp has
// second granularity, so two writes that serialise identical bytes inside the same
// second would otherwise produce the same object: the ref would not move, and a
// compare-and-swap would silently become a no-op.
var ledgerCommitSeq atomic.Uint64

// ledgerCommitMessage renders a commit message that never repeats.
func ledgerCommitMessage() string {
	return fmt.Sprintf("qegg: release ledger %d-%d", time.Now().UnixNano(), ledgerCommitSeq.Add(1))
}

// remoteRef returns the commit the ledger ref points at, or "" when it does not
// exist.
func (s *GitLedgerStore) remoteRef(ctx context.Context) (string, error) {
	res, err := s.Run.Run(ctx, s.Dir, "git", "ls-remote", "--", s.url, s.Ref())
	if err != nil {
		return "", errors.New(i18n.Tf("release.err.ledger_read", s.Describe(), err))
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == s.Ref() {
			return fields[0], nil
		}
	}
	return "", nil
}

// readFile fetches the ledger through the checkout so Git uses its credentials.
func (s *GitLedgerStore) readFile(ctx context.Context, sha string) ([]byte, error) {
	if _, err := s.output(ctx, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--refmap=", "--", s.url, s.Ref()); err != nil {
		return nil, err
	}
	out, err := s.output(ctx, "cat-file", "-p", sha+":"+LedgerPathInRef)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.Tf("release.err.ledger_file", LedgerPathInRef), err)
	}
	return []byte(out), nil
}

// writeCommit stores the ledger in the checkout's object database. A separate
// index keeps the user's staged files out of the ledger tree.
func (s *GitLedgerStore) writeCommit(ctx context.Context, ledger *state.Ledger) (string, error) {
	run, ok := s.Run.(execx.EnvRunner)
	if !ok {
		return "", errors.New(i18n.T("release.err.ledger_runner"))
	}
	data, err := ledger.Marshal()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "qegg-ledger-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	payload := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payload, data, 0o600); err != nil {
		return "", err
	}
	blob, err := s.output(ctx, "hash-object", "-w", "--no-filters", "--", payload)
	if err != nil {
		return "", err
	}

	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
	if _, err := run.RunEnv(ctx, s.Dir, env, "git", "read-tree", "--empty"); err != nil {
		return "", err
	}
	if _, err := run.RunEnv(ctx, s.Dir, env, "git", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+LedgerPathInRef); err != nil {
		return "", err
	}
	tree, err := run.RunEnv(ctx, s.Dir, env, "git", "write-tree")
	if err != nil {
		return "", err
	}
	return s.output(ctx, "-c", "user.name=QuailEgg", "-c", "user.email=qegg@localhost",
		"commit-tree", "--no-gpg-sign", strings.TrimSpace(tree.Stdout), "-m", ledgerCommitMessage())
}

func (s *GitLedgerStore) output(ctx context.Context, args ...string) (string, error) {
	res, err := s.Run.Run(ctx, s.Dir, "git", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
