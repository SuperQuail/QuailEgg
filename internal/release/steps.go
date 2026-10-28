package release

import (
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func step(id string) StepInfo {
	return StepInfo{ID: id, Title: i18n.T("step." + strings.ReplaceAll(id, "-", "_"))}
}

func stepsFor(phase, mode string, hasAssets bool) []StepInfo {
	if phase == PhaseNone {
		return nil
	}
	if phase == PhasePublish {
		return append([]StepInfo{step("preflight")}, publishSteps(hasAssets)...)
	}
	steps := []StepInfo{
		step("preflight"),
		step("verify"),
		step("bump"),
		step("changelog"),
		step("commit"),
	}
	if mode == config.ModeCommit {
		steps = append(steps, publishSteps(hasAssets)...)
	}
	return steps
}

// publishSteps is the shared publish tail. Assets are built before the
// registry upload so a broken build never publishes or tags.
func publishSteps(hasAssets bool) []StepInfo {
	steps := []StepInfo{}
	if hasAssets {
		steps = append(steps, step("assets"))
	}
	return append(steps,
		step("publish"),
		step("tag"),
		step("push"),
		step("github-release"),
	)
}
