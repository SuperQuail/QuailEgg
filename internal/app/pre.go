package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// PreOptions selects the prerelease channel to enter, or leaves it.
type PreOptions struct {
	Channel string
	Exit    bool
}

// PreReport is the outcome of qegg pre.
type PreReport struct {
	Channel string `json:"channel,omitempty"`
	Next    string `json:"next,omitempty"`
	Changed bool   `json:"changed"`
}

// Pre enters, leaves or reports the prerelease channel and previews the next
// version for the resulting state.
func (a *App) Pre(ctx context.Context, opts PreOptions) (*PreReport, error) {
	current, err := state.LoadPreRelease(a.Root)
	if err != nil {
		return nil, err
	}
	switch {
	case opts.Exit:
		if current == nil {
			return nil, errors.New(i18n.T("pre.err.not_active"))
		}
		if err := state.RemovePreRelease(a.Root); err != nil {
			return nil, err
		}
		return a.preReport(ctx, "", true)
	case opts.Channel == "":
		channel := ""
		if current != nil {
			channel = current.Channel
		}
		return a.preReport(ctx, channel, false)
	default:
		if !semver.ValidChannel(opts.Channel) {
			return nil, fmt.Errorf(i18n.T("pre.err.channel"), opts.Channel)
		}
		if current != nil {
			if current.Channel == opts.Channel {
				return a.preReport(ctx, current.Channel, false)
			}
			return nil, fmt.Errorf(i18n.T("pre.err.busy"), current.Channel)
		}
		if err := state.SavePreRelease(a.Root, &state.PreRelease{Channel: opts.Channel}); err != nil {
			return nil, err
		}
		report, err := a.preReport(ctx, opts.Channel, true)
		if err != nil {
			_ = state.RemovePreRelease(a.Root)
			return nil, err
		}
		return report, nil
	}
}

// preReport previews the next version without changing anything else.
func (a *App) preReport(ctx context.Context, channel string, changed bool) (*PreReport, error) {
	plan, err := a.Plan(ctx)
	if err != nil {
		return nil, err
	}
	return &PreReport{Channel: channel, Next: plan.Version.Next, Changed: changed}, nil
}
