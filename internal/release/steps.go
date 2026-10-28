package release

import (
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

func step(id string) StepInfo {
	return StepInfo{ID: id, Title: i18n.T("step." + strings.ReplaceAll(id, "-", "_"))}
}

func stepsFor(phase, mode string) []StepInfo {
	if phase == PhaseNone {
		return nil
	}
	if phase == PhasePublish {
		return []StepInfo{
			step("preflight"),
			step("publish"),
			step("tag"),
			step("push"),
			step("github-release"),
		}
	}
	steps := []StepInfo{
		step("preflight"),
		step("verify"),
		step("bump"),
		step("changelog"),
		step("commit"),
	}
	if mode == config.ModeCommit {
		steps = append(steps,
			step("publish"),
			step("tag"),
			step("push"),
			step("github-release"),
		)
	}
	return steps
}
