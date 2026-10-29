// Package git wraps the git CLI behind an injectable runner.
package git

import (
	"context"
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

// StagedNames lists added or modified files in the index.
func (r *Repo) StagedNames(ctx context.Context) ([]string, error) {
	out, err := r.output(ctx, "diff", "--cached", "--name-only", "--diff-filter=AM")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// ChangedNamesBetween lists files changed between two revisions.
func (r *Repo) ChangedNamesBetween(ctx context.Context, from, to string) ([]string, error) {
	out, err := r.output(ctx, "diff", "--name-only", from+"..."+to)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Add stages paths.
func (r *Repo) Add(ctx context.Context, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.runIn(ctx, append([]string{"add", "--"}, paths...)...)
	return err
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

// CreateTag creates an annotated tag.
func (r *Repo) CreateTag(ctx context.Context, name, message string) error {
	_, err := r.runIn(ctx, "tag", "-a", name, "-m", message)
	return err
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
