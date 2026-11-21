// Package git wraps the git CLI behind an injectable runner.
package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// Repo is a working tree bound to a runner.
type Repo struct {
	root string
	run  execx.Runner
}

// Open locates the repository containing dir.
func Open(ctx context.Context, dir string, run execx.Runner) (*Repo, error) {
	res, err := run.Run(ctx, dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	root := strings.TrimSpace(res.Stdout)
	if root == "" {
		return nil, fmt.Errorf("git rev-parse returned an empty root")
	}
	return &Repo{root: root, run: run}, nil
}

// Root returns the repository root.
func (r *Repo) Root() string { return r.root }

func (r *Repo) runIn(ctx context.Context, args ...string) (execx.Result, error) {
	return r.run.Run(ctx, r.root, "git", args...)
}

func (r *Repo) output(ctx context.Context, args ...string) (string, error) {
	res, err := r.runIn(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// Branch returns the current branch name. A repository without commits has no
// HEAD yet, so the symbolic ref is used as a fallback.
func (r *Repo) Branch(ctx context.Context) (string, error) {
	name, err := r.output(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		return name, nil
	}
	return r.output(ctx, "symbolic-ref", "--short", "HEAD")
}

// HeadSHA returns the current commit hash.
func (r *Repo) HeadSHA(ctx context.Context) (string, error) {
	return r.output(ctx, "rev-parse", "HEAD")
}

// IsClean reports whether the working tree has no tracked or untracked changes.
func (r *Repo) IsClean(ctx context.Context) (bool, error) {
	return r.IsCleanExcept(ctx, nil)
}

// IsCleanExcept reports whether the working tree has no tracked or untracked
// changes outside the excluded repository-relative paths.
func (r *Repo) IsCleanExcept(ctx context.Context, exclude []string) (bool, error) {
	args := []string{"status", "--porcelain"}
	if len(exclude) > 0 {
		args = append(args, "--", ".")
		for _, path := range exclude {
			args = append(args, ":!"+path)
		}
	}
	out, err := r.output(ctx, args...)
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// TagExists reports whether name exists.
func (r *Repo) TagExists(ctx context.Context, name string) (bool, error) {
	_, err := r.runIn(ctx, "rev-parse", "--verify", "--quiet", "refs/tags/"+name)
	if err != nil {
		if exitErr, ok := err.(*execx.Error); ok && exitErr.Result.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// TagPointsAt reports whether the tag resolves to the given commit.
func (r *Repo) TagPointsAt(ctx context.Context, name, sha string) (bool, error) {
	out, err := r.output(ctx, "rev-list", "-n", "1", name)
	if err != nil {
		return false, err
	}
	if out == sha {
		return true, nil
	}
	head, headErr := r.output(ctx, "rev-parse", sha+"^{commit}")
	if headErr != nil {
		return false, nil
	}
	return out == head, nil
}

// CheckoutBranch creates or resets a local branch at the current commit.
func (r *Repo) CheckoutBranch(ctx context.Context, name string) error {
	_, err := r.runIn(ctx, "checkout", "-B", name)
	return err
}

// RemoteBranchExists reports whether the remote has the named branch.
func (r *Repo) RemoteBranchExists(ctx context.Context, remote, branch string) (bool, error) {
	out, err := r.output(ctx, "ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// CommitSubject returns the subject line of a commit.
func (r *Repo) CommitSubject(ctx context.Context, sha string) (string, error) {
	return r.output(ctx, "log", "-1", "--format=%s", sha)
}

// CommitAuthor returns the author name of a commit.
func (r *Repo) CommitAuthor(ctx context.Context, sha string) (string, error) {
	return r.output(ctx, "log", "-1", "--format=%an", sha)
}

// ErrPathMissing reports that a revision or the index does not carry the path.
// It lets a caller tell a deleted file from a git failure.
var ErrPathMissing = errors.New("path is not in the snapshot")

// ErrNoCommonHistory reports that the two revisions of a range share no history,
// so there is no merge base to compare from. git fails the diff the same way for
// a revision it cannot read, so the range is asked for its merge base first.
var ErrNoCommonHistory = errors.New("revisions share no history")

// FileChange is one path that differs between two snapshots.
type FileChange struct {
	Path string
	// Status is the git status letter: A added, M modified, D deleted,
	// T type changed.
	Status string
}

// StagedChanges lists the index changes with their status, which is the snapshot
// the next commit will contain.
func (r *Repo) StagedChanges(ctx context.Context) ([]FileChange, error) {
	res, err := r.runIn(ctx, "diff", "--cached", "--name-status", "--no-renames", "-z")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(res.Stdout), nil
}

// ChangedFiles lists the paths a revision range changes, with their status. The
// range is written like "base...head" for a pull request or "before..after" for
// a push.
func (r *Repo) ChangedFiles(ctx context.Context, revspec string) ([]FileChange, error) {
	if strings.TrimSpace(revspec) == "" {
		return nil, errors.New("empty revision range")
	}
	res, err := r.runIn(ctx, "diff", "--name-status", "--no-renames", "-z", revspec)
	if err != nil {
		if r.noCommonHistory(ctx, revspec) {
			return nil, fmt.Errorf("%w: %s", ErrNoCommonHistory, revspec)
		}
		return nil, err
	}
	return parseNameStatus(res.Stdout), nil
}

// noCommonHistory reports whether a failed range failed because its revisions
// share no history rather than because git could not read one of them. git
// merge-base exits 1 without output for the former and 128 for the latter.
func (r *Repo) noCommonHistory(ctx context.Context, revspec string) bool {
	from, to, ok := strings.Cut(revspec, "...")
	if !ok {
		return false
	}
	res, err := r.runIn(ctx, "merge-base", from, to)
	if err != nil {
		var exitErr *execx.Error
		return errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1
	}
	return strings.TrimSpace(res.Stdout) == ""
}

// IndexBlob reads path from the index: the content the commit will contain, not
// the working tree copy. It reports ErrPathMissing when the index does not carry
// the path, which is how a staged deletion looks.
func (r *Repo) IndexBlob(ctx context.Context, path string) ([]byte, error) {
	found, err := r.hasPath(ctx, "ls-files", "--cached", "-z", "--", literal(path))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrPathMissing
	}
	return r.blob(ctx, ":"+path)
}

// TreeBlob reads path as it exists in rev. It reports ErrPathMissing when that
// revision has no such path.
func (r *Repo) TreeBlob(ctx context.Context, rev, path string) ([]byte, error) {
	found, err := r.hasPath(ctx, "ls-tree", "--name-only", "-z", rev, "--", literal(path))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrPathMissing
	}
	return r.blob(ctx, rev+":"+path)
}

// blob reads one object as it is stored, without trimming the content.
func (r *Repo) blob(ctx context.Context, object string) ([]byte, error) {
	res, err := r.runIn(ctx, "show", object)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

// hasPath reports whether a git listing shows the path. The listing commands
// report a missing path as empty output instead of an error, which keeps a
// deleted file apart from a repository git cannot read.
func (r *Repo) hasPath(ctx context.Context, args ...string) (bool, error) {
	res, err := r.runIn(ctx, args...)
	if err != nil {
		return false, err
	}
	return strings.Trim(res.Stdout, "\x00") != "", nil
}

// literal marks path as a literal pathspec, so a file name with glob characters
// matches itself and nothing else.
func literal(path string) string {
	return ":(literal)" + path
}

// parseNameStatus reads the NUL separated status and path pairs git prints for
// --name-status -z. Renames are disabled by the callers, so every entry is one
// status and one path; a path keeps tabs and spaces intact.
func parseNameStatus(out string) []FileChange {
	fields := strings.Split(out, "\x00")
	changes := make([]FileChange, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		status, name := fields[i], fields[i+1]
		if status == "" || name == "" {
			break
		}
		// A copied or renamed entry carries a score such as R100; the rule only
		// needs the letter.
		changes = append(changes, FileChange{Path: name, Status: status[:1]})
	}
	return changes
}

// Add stages paths.
func (r *Repo) Add(ctx context.Context, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.runIn(ctx, append([]string{"add", "--"}, paths...)...)
	return err
}

// Ignored reports whether path is excluded by the repository ignore rules.
// Tracked files are never ignored, so a committed manifest still stages.
func (r *Repo) Ignored(ctx context.Context, path string) (bool, error) {
	_, err := r.runIn(ctx, "check-ignore", "-q", "--", path)
	if err != nil {
		if exitErr, ok := err.(*execx.Error); ok && exitErr.Result.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Remove stages deletions of paths.
func (r *Repo) Remove(ctx context.Context, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.runIn(ctx, append([]string{"rm", "--quiet", "--"}, paths...)...)
	return err
}

// Commit creates a commit and returns its hash.
func (r *Repo) Commit(ctx context.Context, message string) (string, error) {
	return r.commit(ctx, message, false)
}

// CommitNoVerify creates a commit without running repository hooks. Release
// commits write versions and changelogs themselves and must not be blocked by
// the change record rule.
func (r *Repo) CommitNoVerify(ctx context.Context, message string) (string, error) {
	return r.commit(ctx, message, true)
}

func (r *Repo) commit(ctx context.Context, message string, noVerify bool) (string, error) {
	args := []string{"commit"}
	if noVerify {
		args = append(args, "--no-verify")
	}
	args = append(args, "-m", message)
	if _, err := r.runIn(ctx, args...); err != nil {
		return "", err
	}
	return r.HeadSHA(ctx)
}

// CreateTag creates an annotated tag at HEAD.
func (r *Repo) CreateTag(ctx context.Context, name, message string) error {
	return r.CreateTagAt(ctx, name, message, "HEAD")
}

// CreateTagAt creates an annotated tag at an explicit commit. A release that
// resumes after the branch moved must tag the snapshot it published, not HEAD.
func (r *Repo) CreateTagAt(ctx context.Context, name, message, sha string) error {
	_, err := r.runIn(ctx, "tag", "-a", name, "-m", message, sha)
	return err
}

// ResolveCommit resolves a revision to the commit it names.
func (r *Repo) ResolveCommit(ctx context.Context, rev string) (string, error) {
	return r.output(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
}

// FileAtCommit returns the content of path at rev. found is false when the
// commit has no such path; an unreadable revision or object is an error and
// never reads as a missing file.
func (r *Repo) FileAtCommit(ctx context.Context, rev, path string) ([]byte, bool, error) {
	entry, err := r.output(ctx, "ls-tree", rev, "--", path)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(entry) == "" {
		return nil, false, nil
	}
	res, err := r.runIn(ctx, "cat-file", "blob", rev+":"+path)
	if err != nil {
		return nil, false, err
	}
	return []byte(res.Stdout), true, nil
}

// ParentCommit returns the first parent of a commit, which is the parent the
// first-parent history follows. found is false for a root commit.
func (r *Repo) ParentCommit(ctx context.Context, rev string) (string, bool, error) {
	out, err := r.output(ctx, "rev-list", "--parents", "-n", "1", rev)
	if err != nil {
		return "", false, err
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return "", false, nil
	}
	return fields[1], true, nil
}

// IsAncestor reports whether ancestor is reachable from rev. A commit that is
// not reachable is not a snapshot this branch can build from.
func (r *Repo) IsAncestor(ctx context.Context, ancestor, rev string) (bool, error) {
	_, err := r.runIn(ctx, "merge-base", "--is-ancestor", ancestor, rev)
	if err != nil {
		var exitErr *execx.Error
		if errors.As(err, &exitErr) && exitErr.Result.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsShallow reports whether the repository is a shallow clone. A shallow clone
// is missing the history a release snapshot has to be located in, so the
// executor refuses to guess there.
func (r *Repo) IsShallow(ctx context.Context) (bool, error) {
	out, err := r.output(ctx, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

// CommitsTouching lists the commits in the first-parent history of HEAD that
// changed path, newest first. First-parent matters for a release branch merged
// with a merge commit: the merge carries the merged tree, while the branch
// commit itself is not on the target branch.
func (r *Repo) CommitsTouching(ctx context.Context, path string) ([]string, error) {
	out, err := r.output(ctx, "log", "--first-parent", "--format=%H", "--", path)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// AddWorktree checks rev out in a detached worktree at dir. The executor builds
// and uploads from that worktree so a branch that moved on cannot leak into the
// release.
func (r *Repo) AddWorktree(ctx context.Context, dir, rev string) error {
	_, err := r.runIn(ctx, "worktree", "add", "--detach", "--force", dir, rev)
	return err
}

// RemoveWorktree removes a worktree created by AddWorktree.
func (r *Repo) RemoveWorktree(ctx context.Context, dir string) error {
	_, err := r.runIn(ctx, "worktree", "remove", "--force", dir)
	return err
}

// PruneWorktrees drops worktree registrations whose directory is gone, so a
// cleanup that failed once does not leave the repository pointing at it.
func (r *Repo) PruneWorktrees(ctx context.Context) {
	_, _ = r.runIn(ctx, "worktree", "prune")
}

// Push pushes refs to the remote.
func (r *Repo) Push(ctx context.Context, remote string, refs ...string) error {
	args := append([]string{"push", remote}, refs...)
	_, err := r.runIn(ctx, args...)
	return err
}

// RemoteURL returns the configured URL of a remote.
func (r *Repo) RemoteURL(ctx context.Context, remote string) (string, error) {
	return r.output(ctx, "remote", "get-url", remote)
}

// IntroducingCommit returns the commit that first added path.
func (r *Repo) IntroducingCommit(ctx context.Context, path string) (string, error) {
	out, err := r.output(ctx, "log", "--reverse", "--diff-filter=A", "--format=%H", "--", path)
	if err != nil {
		return "", err
	}
	lines := splitLines(out)
	if len(lines) == 0 {
		out, err = r.output(ctx, "log", "--follow", "--reverse", "--diff-filter=A", "--format=%H", "--", path)
		if err != nil {
			return "", err
		}
		lines = splitLines(out)
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("no commit found for %s", path)
	}
	return lines[0], nil
}

func splitLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// ListTags returns the tags reachable from HEAD that start with prefix.
func (r *Repo) ListTags(ctx context.Context, prefix string) ([]string, error) {
	out, err := r.output(ctx, "tag", "--merged", "HEAD", "--list", prefix+"*")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		if tag := strings.TrimSpace(line); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

// RemoteTagCommit returns the commit a tag points at on the remote. found is
// false when the remote has no such tag; an unreachable remote is an error and
// never reads as a missing tag, because "missing" would push over a tag that is
// already there.
func (r *Repo) RemoteTagCommit(ctx context.Context, remote, tag string) (string, bool, error) {
	ref := "refs/tags/" + tag
	res, err := r.runIn(ctx, "ls-remote", remote, ref, ref+"^{}")
	if err != nil {
		return "", false, err
	}
	plain, peeled := "", ""
	for _, line := range strings.Split(res.Stdout, "\n") {
		// ls-remote writes "<hash>\t<ref>"; splitting on the tab keeps a ref
		// name whole instead of re-splitting it on whitespace.
		hash, name, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if !ok {
			continue
		}
		switch name {
		case ref + "^{}":
			peeled = hash
		case ref:
			plain = hash
		}
	}
	if peeled != "" {
		return peeled, true, nil
	}
	if plain == "" {
		return "", false, nil
	}
	// A lightweight tag already names the commit; an annotated tag whose peeled
	// line the server did not advertise names a tag object, so resolve it
	// locally when the object exists and keep the hash otherwise.
	if commit, err := r.ResolveCommit(ctx, plain); err == nil {
		return commit, true, nil
	}
	return plain, true, nil
}

// TagCommit returns the commit a local tag points at. It is the local counterpart
// of RemoteTagCommit: a release compares the two before it touches anything.
func (r *Repo) TagCommit(ctx context.Context, name string) (string, error) {
	return r.output(ctx, "rev-list", "-n", "1", "refs/tags/"+name)
}
