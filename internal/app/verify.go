package app

import (
	"context"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/verify"
	"os"
	"time"
)

// Verify runs the configured pre-release commands.
func (a *App) Verify(ctx context.Context) error {
	timeout, err := time.ParseDuration(a.Cfg.Verify.Timeout)
	if err != nil {
		timeout = 15 * time.Minute
	}
	result, err := verify.RunVerification(ctx, a.Run, a.Root, a.Cfg.Verify.Commands, timeout, a.Cfg.Verify.FailFast, os.Stdout)
	if err != nil {
		return err
	}
	if len(result.Commands) == 0 {
		a.UI.Warn("%s", i18n.T("verify.none_configured"))
		return nil
	}
	a.UI.Success("%s", i18n.Tf("verify.passed", result.Duration.Round(time.Millisecond)))
	return nil
}
