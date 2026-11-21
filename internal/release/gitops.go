package release

import "context"

// GitOps is the narrow git surface the release executor needs. It lets the
// executor depend on an interface instead of the concrete CLI wrapper.
type GitOps interface {
	IsClean(ctx context.Context) (bool, error)
	IsCleanExcept(ctx context.Context, exclude []string) (bool, error)
	Branch(ctx context.Context) (string, error)
	HeadSHA(ctx context.Context) (string, error)
	TagExists(ctx context.Context, name string) (bool, error)
	ListTags(ctx context.Context, prefix string) ([]string, error)
	TagPointsAt(ctx context.Context, name, sha string) (bool, error)
	CreateTag(ctx context.Context, name, message string) error
	CreateTagAt(ctx context.Context, name, message, sha string) error
	ResolveCommit(ctx context.Context, rev string) (string, error)
	RemoteTagCommit(ctx context.Context, remote, tag string) (string, bool, error)
	FileAtCommit(ctx context.Context, rev, path string) ([]byte, bool, error)
	CommitsTouching(ctx context.Context, path string) ([]string, error)
	ParentCommit(ctx context.Context, rev string) (string, bool, error)
	IsShallow(ctx context.Context) (bool, error)
	IsAncestor(ctx context.Context, ancestor, rev string) (bool, error)
	AddWorktree(ctx context.Context, dir, rev string) error
	RemoveWorktree(ctx context.Context, dir string) error
	PruneWorktrees(ctx context.Context)
	Push(ctx context.Context, remote string, refs ...string) error
	Add(ctx context.Context, paths ...string) error
	Remove(ctx context.Context, paths ...string) error
	Ignored(ctx context.Context, path string) (bool, error)
	Commit(ctx context.Context, message string) (string, error)
	CommitNoVerify(ctx context.Context, message string) (string, error)
	IntroducingCommit(ctx context.Context, path string) (string, error)
	CheckoutBranch(ctx context.Context, name string) error
	RemoteBranchExists(ctx context.Context, remote, branch string) (bool, error)
	CommitSubject(ctx context.Context, sha string) (string, error)
	CommitAuthor(ctx context.Context, sha string) (string, error)
	// TagCommit reports the commit a local tag points at.
	TagCommit(ctx context.Context, name string) (string, error)
}
