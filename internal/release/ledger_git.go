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

// ledgerTemplate is the placeholder the ledger lives in: an empty temporary git
// directory used to build the ledger commit without touching the worktree index.
const ledgerTemplate = "qegg-ledger-*"

// GitLedgerStore keeps the ledger in a git commit under its release ref.
// Every write is a compare-and-swap: the push is accepted only while the remote
// ref still holds the revision this run read, so two concurrent resumes cannot
// both record the same package as "started".
type GitLedgerStore struct {
	Run    execx.Runner
	Remote string
	// Dir is the working directory the git commands run in.
	Dir string

	url   string
	tag   string
	empty string
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

// Close removes the temporary git directory used to build ledger commits.
func (s *GitLedgerStore) Close() {
	if s.empty != "" {
		os.RemoveAll(s.empty)
		s.empty = ""
	}
}

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
	dir, err := s.gitDir(ctx)
	if err != nil {
		return "", err
	}
	data, err := ledger.Marshal()
	if err != nil {
		return "", err
	}
	payload := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payload, data, 0o644); err != nil {
		return "", err
	}
	defer os.Remove(payload)

	blob, err := s.output(ctx, dir, "hash-object", "-w", payload)
	if err != nil {
		return "", err
	}
	if _, err := s.output(ctx, dir, "read-tree", "--empty"); err != nil {
		return "", err
	}
	if _, err := s.output(ctx, dir, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+LedgerPathInRef); err != nil {
		return "", err
	}
	tree, err := s.output(ctx, dir, "write-tree")
	if err != nil {
		return "", err
	}
	commit, err := s.output(ctx, dir, "-c", "user.name=QuailEgg", "-c", "user.email=qegg@localhost",
		"commit-tree", tree, "-m", ledgerCommitMessage())
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
	_, pushErr := s.output(ctx, dir, "push", lease, s.url, commit+":"+s.Ref())
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
	res, err := s.Run.Run(ctx, s.Dir, "git", "ls-remote", s.url, s.Ref())
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

// readFile reads the ledger file from a fetched ledger commit.
func (s *GitLedgerStore) readFile(ctx context.Context, sha string) ([]byte, error) {
	dir, err := s.gitDir(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.output(ctx, dir, "fetch", "--depth=1", "--no-tags", s.url, s.Ref()); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("release.err.ledger_read"), err)
	}
	out, err := s.output(ctx, dir, "cat-file", "-p", sha+":"+LedgerPathInRef)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.Tf("release.err.ledger_file", LedgerPathInRef), err)
	}
	return []byte(out), nil
}

// gitDir creates the throwaway git directory the ledger commits are built in.
func (s *GitLedgerStore) gitDir(ctx context.Context) (string, error) {
	if s.empty != "" {
		return s.empty, nil
	}
	dir, err := os.MkdirTemp("", ledgerTemplate)
	if err != nil {
		return "", err
	}
	if _, err := s.Run.Run(ctx, "", "git", "init", "--bare", "-q", dir); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	s.empty = dir
	return dir, nil
}

// output runs one git command inside the ledger directory and returns its
// trimmed standard output.
func (s *GitLedgerStore) output(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"--git-dir=" + dir}, args...)
	res, err := s.Run.Run(ctx, dir, "git", full...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
