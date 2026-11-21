package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// remoteURL resolves the URL of one configured remote.
func remoteURL(ctx context.Context, run execx.Runner, root, remote string) (string, error) {
	res, err := run.Run(ctx, root, "git", "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// remoteTag is the state of one tag on the release remote. Commit is the commit
// the tag resolves to, which is what a resume has to compare: an annotated tag
// has its own object hash, so comparing tag hashes would reject a correct tag.
//
// TODO(merge): replace readRemoteTag with git.Repo.RemoteTagCommit (PR-04) once
// that lands; the identifiers here are deliberately unique to this branch so the
// two additions can be merged as a union.
type remoteTag struct {
	Present bool
	Commit  string
}

// readRemoteTag asks the remote for one tag. A missing tag is reported as such;
// a failed lookup is an error, never "missing".
func readRemoteTag(ctx context.Context, run execx.Runner, root, remote, tag string) (remoteTag, error) {
	ref := "refs/tags/" + tag
	res, err := run.Run(ctx, root, "git", "ls-remote", remote, ref, ref+"^{}")
	if err != nil {
		return remoteTag{}, fmt.Errorf("%s: %w", i18n.Tf("release.err.remote_tag_read", tag, remote), err)
	}
	var answer remoteTag
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[1] {
		case ref:
			answer.Present = true
			if answer.Commit == "" {
				answer.Commit = fields[0]
			}
		case ref + "^{}":
			answer.Present = true
			answer.Commit = fields[0]
		}
	}
	return answer, nil
}

// pushTag pushes one tag that the remote does not have yet. The push is not
// forced: a remote tag that already exists with another target makes git refuse,
// and a refusal stops the release instead of overwriting a published reference.
func pushTag(ctx context.Context, run execx.Runner, root, remote, tag string) error {
	ref := "refs/tags/" + tag
	if _, err := run.Run(ctx, root, "git", "push", remote, ref+":"+ref); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("release.err.tag_push", tag, remote), err)
	}
	return nil
}

// createTagAt creates one annotated tag on an explicit commit. The release
// source commit is not always HEAD: a resume that runs after main moved on must
// still tag the commit the release was planned for.
func createTagAt(ctx context.Context, run execx.Runner, root, tag, sha, message string) error {
	args := []string{"tag", "-a", tag}
	if sha != "" {
		args = append(args, sha)
	}
	args = append(args, "-m", message)
	if _, err := run.Run(ctx, root, "git", args...); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("release.err.tag_create", tag), err)
	}
	return nil
}

