package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/github"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/scaffold"
	"github.com/SuperQuail/QuailEgg/internal/verify"
)

// HookPreCommit enforces the change record rule and then runs the commit-time
// verification tier when the commit touches release-scoped files, so a failing
// test blocks the commit unless the tier is turned off in the configuration.
func (a *App) HookPreCommit(ctx context.Context) error {
	staged, err := a.Git.StagedNames(ctx)
	if err != nil {
		return err
	}
	if err := a.checkRecordRule(ctx, staged); err != nil {
		return err
	}
	execx.SanitizeGitHookEnv()
	if !a.Cfg.Verify.HookEnabled() {
		if a.touchesScope(staged) {
			a.UI.Info("%s", i18n.T("hook.verify_disabled"))
		}
		return nil
	}
	if len(a.Cfg.Verify.HookCommandList()) == 0 {
		return nil
	}
	if !a.touchesScope(staged) {
		a.UI.Info("%s", i18n.T("hook.verify_skipped"))
		return nil
	}
	return a.verifyBeforeCommit(ctx)
}

// touchesScope reports whether the staged files include anything the release
// pipeline watches. Managed files and change records never count, so
// documentation-only or record-only commits skip the verification commands.
func (a *App) touchesScope(staged []string) bool {
	ignore := scaffold.ManagedPaths(a.Cfg.Changes.Dir)
	return changes.TouchesScope(staged, a.Cfg.Changes.Dir, a.Cfg.Changes.Scope, ignore)
}

// checkRecordRule enforces the record rule only where it is knowable: the
// default branch of the project repository. Feature branches and fork clones
// are left to the pull request check, which sees the whole diff.
func (a *App) checkRecordRule(ctx context.Context, staged []string) error {
	ignore := scaffold.ManagedPaths(a.Cfg.Changes.Dir)
	if len(staged) == 0 {
		return nil
	}
	branch, err := a.Git.Branch(ctx)
	if err != nil {
		return err
	}
	touched := changes.TouchesScope(staged, a.Cfg.Changes.Dir, a.Cfg.Changes.Scope, ignore)
	if branch != a.Cfg.Project.DefaultBranch {
		if touched {
			a.UI.Warn("%s", i18n.T("hook.branch_skip"))
		}
		return nil
	}
	if project, current := a.isProjectClone(ctx); !project {
		if touched {
			a.UI.Warn("%s", i18n.Tf("hook.fork_skip", current, a.Cfg.Project.Repo))
		}
		return nil
	}
	return changes.Enforce(changes.Staged{
		Root:   a.Root,
		Dir:    a.Cfg.Changes.Dir,
		Scope:  a.Cfg.Changes.Scope,
		Ignore: ignore,
		Known:  a.Cfg.PackageNames(),
		Files:  staged,
	})
}

// isProjectClone reports whether the current clone points at the repository
// recorded in the configuration, and returns the identity it observed. An
// unknown identity is treated as the project so old configurations keep the
// strict behaviour they had.
func (a *App) isProjectClone(ctx context.Context) (bool, string) {
	expected := strings.TrimSpace(a.Cfg.Project.Repo)
	if expected == "" {
		return true, ""
	}
	res, err := a.Run.Run(ctx, a.Root, "git", "remote", "get-url", a.Cfg.Git.Remote)
	if err != nil {
		return true, ""
	}
	repo, ok := github.ParseRemote(res.Stdout)
	if !ok {
		return true, ""
	}
	current := repo.ID()
	return strings.EqualFold(current, expected), current
}

// verifyBeforeCommit runs the commit-time verification tier: HookCommands when
// configured, otherwise the release commands. The release pipeline always runs
// the full command list before bumping a version.
func (a *App) verifyBeforeCommit(ctx context.Context) error {
	commands := a.Cfg.Verify.HookCommandList()
	if len(commands) == 0 {
		return nil
	}
	timeoutValue := a.Cfg.Verify.HookTimeout
	if timeoutValue == "" {
		timeoutValue = a.Cfg.Verify.Timeout
	}
	timeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		timeout = 15 * time.Minute
	}
	result, err := verify.RunVerification(ctx, a.Run, a.Root, commands, timeout, a.Cfg.Verify.FailFast, os.Stdout)
	if err != nil {
		failed := result.FailedCommand
		if failed == "" {
			failed = commands[0]
		}
		return errors.New(i18n.Tf("hook.verify_failed", failed))
	}
	return nil
}
