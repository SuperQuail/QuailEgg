package release

import (
	"context"
	"fmt"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"os"
	"path/filepath"
	"time"
)

type executor struct {
	d         *Deps
	plan      *Plan
	run       *state.RunRecord
	syncPaths []string
}

// Run executes the plan and records progress in local state.
func Run(ctx context.Context, d *Deps, plan *Plan) error {
	if plan.Phase == PhaseNone {
		d.UI.Info("%s", i18n.T("release.nothing"))
		return nil
	}
	runID := fmt.Sprintf("%s-%d", d.Now().Format("20060102T150405"), d.Now().UnixNano()%10000)
	run := state.NewRunRecord(runID, d.Cfg.Release.Mode, plan.Phase, plan.Fingerprint)
	run.Qegg = d.toolInfo()
	e := &executor{d: d, plan: plan, run: run}
	return e.execute(ctx)
}

func (e *executor) execute(ctx context.Context) error {
	var err error
	if e.plan.Phase == PhaseVersion {
		err = e.runVersion(ctx)
	} else {
		err = e.runPublish(ctx)
	}
	if err != nil {
		e.run.Status = state.StatusFailed
		state.SaveRun(e.d.Root, e.run)
		return err
	}
	e.run.Status = state.StatusDone
	state.SaveRun(e.d.Root, e.run)
	return nil
}

func (e *executor) step(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	start := time.Now()
	e.run.Mark(name, state.StatusRunning, nil)
	if err := fn(ctx); err != nil {
		e.run.Mark(name, state.StatusFailed, err)
		return err
	}
	e.run.MarkDuration(name, state.StatusDone, time.Since(start))
	return state.SaveRun(e.d.Root, e.run)
}

func (e *executor) skip(name, reason string) {
	e.run.Mark(name, state.StatusSkipped, nil)
	e.d.UI.Info("%s", i18n.Tf("release.skip", name, reason))
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func isCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}

func filepathRel(root, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
