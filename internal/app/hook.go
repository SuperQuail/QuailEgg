package app

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/scaffold"
	"github.com/SuperQuail/QuailEgg/internal/verify"
)

// HookPreCommit enforces the change record rule and then runs the verification
// commands, so a failing test blocks the commit.
func (a *App) HookPreCommit(ctx context.Context) error {
	staged, err := a.Git.StagedNames(ctx)
	if err != nil {
		return err
	}
	if err := changes.Enforce(changes.Staged{
		Root:   a.Root,
		Dir:    a.Cfg.Changes.Dir,
		Scope:  a.Cfg.Changes.Scope,
		Ignore: scaffold.ManagedPaths(a.Cfg.Changes.Dir),
		Known:  a.Cfg.PackageNames(),
		Files:  staged,
	}); err != nil {
		return err
	}
	return a.verifyBeforeCommit(ctx)
}

// verifyBeforeCommit runs [verify].commands. The release pipeline runs the same
// commands before bumping; the hook makes them a commit-time gate as well.
func (a *App) verifyBeforeCommit(ctx context.Context) error {
	if len(a.Cfg.Verify.Commands) == 0 {
		return nil
	}
	timeout, err := time.ParseDuration(a.Cfg.Verify.Timeout)
	if err != nil {
		timeout = 15 * time.Minute
	}
	result, err := verify.RunVerification(ctx, a.Run, a.Root, a.Cfg.Verify.Commands, timeout, a.Cfg.Verify.FailFast, os.Stdout)
	if err != nil {
		failed := result.FailedCommand
		if failed == "" {
			failed = a.Cfg.Verify.Commands[0]
		}
		return errors.New(i18n.Tf("hook.verify_failed", failed))
	}
	return nil
}
